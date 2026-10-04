"""Offline proof-client security/failure tests; not native ServiceNow evidence."""
import contextlib
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import tempfile
import time
import unittest
from unittest.mock import patch
import urllib.error
import urllib.request

from test_servicenow_update_set import complete_fixture

spec = importlib.util.spec_from_file_location('export_proof', Path(__file__).with_name('probe-servicenow-xml-export.py'))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class FakeSession:
    def __init__(self, state='complete', body=None):
        self.state = state
        self.body = complete_fixture() if body is None else body
        self.calls = []

    def rows(self, table, query, fields):
        if table == 'sys_app':
            return [{'sys_id': m.APP, 'scope': m.SCOPE, 'version': '0.4.6'}]
        return [{'sys_id': 'b' * 32, 'state': self.state, 'application': m.APP}]

    def ajax(self, params):
        self.calls.append(params)
        return 'b' * 32

    def download(self, set_id):
        return self.body


class Response(io.BytesIO):
    def __init__(self, body=b'', status=200, headers=None):
        super().__init__(body)
        self.status = status
        self.headers = headers or {}


class ExportProofTests(unittest.TestCase):
    def test_destination_is_fixed_and_https_only(self):
        for origin in ['http://dev394887.service-now.com', m.ORIGIN + '/',
                       'https://dev317694.service-now.com', 'https://admin:password@dev394887.service-now.com']:
            with self.subTest(origin=origin), self.assertRaises(m.Rejected):
                m.Session(origin)

    def test_network_redirect_is_not_followed(self):
        handler = m.NoRedirect()
        self.assertIsNone(handler.redirect_request(None, None, 302, '', {}, 'https://evil.example'))
        s = m.Session(m.ORIGIN)
        with patch.object(s.opener, 'open', side_effect=urllib.error.HTTPError(
                m.ORIGIN, 302, 'secret-response', {'Location': 'https://evil.example'}, io.BytesIO())) as request:
            with self.assertRaises(m.Rejected):
                s.request('/angular.do', {'user_password': 'sentinel-secret'})
            self.assertEqual(request.call_count, 1)

    def test_read_limit_and_expired_deadline(self):
        s = m.Session(m.ORIGIN)
        with patch.object(m, 'MAX_BYTES', 10), patch.object(s.opener, 'open', return_value=Response(b'x' * 11)):
            with self.assertRaises(m.Rejected):
                s.request('/xmlhttp.do')
        s.deadline = time.monotonic() - 1
        with patch.object(s.opener, 'open') as request:
            with self.assertRaises(m.Rejected):
                s.request('/xmlhttp.do')
            request.assert_not_called()

    def test_login_requires_success_and_csrf(self):
        for body in [b'{"status":"error"}', b'{"status":"mfa_code_required"}', b'{}']:
            s = m.Session(m.ORIGIN)
            with patch.object(s, 'request', return_value=(200, {}, body)) as request:
                with self.assertRaises(m.Rejected):
                    s.login('admin', 'sentinel-secret')
                self.assertEqual(request.call_count, 1)
        s = m.Session(m.ORIGIN)
        with patch.object(s, 'request', side_effect=[(200, {}, b'{"status":"success"}'), (401, {}, b'')]):
            with self.assertRaises(m.Rejected):
                s.login('admin', 'sentinel-secret')
        with patch.object(s, 'request', side_effect=[(200, {}, b'{"status":"success"}'),
                (401, {'x-usertoken-response': 'a' * 64}, b'')]):
            s.login('admin', 'sentinel-secret')
            self.assertEqual(s.token, 'a' * 64)

    def test_download_restricts_redirect_target(self):
        bad = ['https://evil.example/export_update_set.do?sysparm_sys_id=' + 'a' * 32,
               '/login.do', '//evil.example/export_update_set.do',
               '/export_update_set.do?sysparm_sys_id=bad',
               '/export_update_set.do?sysparm_sys_id=' + 'a' * 32 + '&extra=secret',
               '/export_update_set.do?sysparm_sys_id=' + 'a' * 32 + '&sysparm_sys_id=' + 'b' * 32]
        for location in bad:
            s = m.Session(m.ORIGIN)
            with patch.object(s, 'request', return_value=(302, {'Location': location}, b'')) as request:
                with self.subTest(location=location), self.assertRaises(m.Rejected):
                    s.download('b' * 32)
                self.assertEqual(request.call_count, 1)
        s = m.Session(m.ORIGIN)
        location = '/export_update_set.do?sysparm_sys_id=' + 'a' * 32
        with patch.object(s, 'request', side_effect=[(302, {'Location': location}, b''), (200, {}, b'xml')]):
            self.assertEqual(s.download('b' * 32), b'xml')

    def test_ajax_rejects_entities_and_wrong_root(self):
        for body in [b'<!DOCTYPE xml><xml/>', b'<html/>', b'<!ENTITY x "secret"><xml/>']:
            s = m.Session(m.ORIGIN)
            with patch.object(s, 'request', return_value=(200, {}, body)):
                with self.subTest(body=body), self.assertRaises(m.Rejected):
                    s.ajax({})

    def test_success_preserves_bytes_but_not_a_release_claim(self):
        session = FakeSession()
        with tempfile.TemporaryDirectory() as tmp:
            report = {}
            m.run(session, Path(tmp), report, pause=lambda _: None)
            self.assertEqual(report['status'], 'passed')
            self.assertFalse(report['customer_release'])
            self.assertEqual(report['source_equivalence'], 'not_checked')
            self.assertEqual((Path(tmp) / 'raw-export.xml').read_bytes(), session.body)
            self.assertEqual(report['xml_sha256'], hashlib.sha256(session.body).hexdigest())
            self.assertEqual(session.calls[1]['sysparm_include_data'], 'false')
            self.assertEqual((Path(tmp) / 'raw-export.xml').stat().st_mode & 0o777, 0o600)

    def test_ambiguous_publication_not_retried_and_id_retained(self):
        session = FakeSession()
        original = session.ajax
        def fail_second(params):
            value = original(params)
            if len(session.calls) == 2:
                raise TimeoutError('sentinel-secret')
            return value
        session.ajax = fail_second
        with tempfile.TemporaryDirectory() as tmp:
            report = {}
            with self.assertRaises(TimeoutError):
                m.run(session, Path(tmp), report)
            self.assertEqual(len(session.calls), 2)
            self.assertEqual(json.loads((Path(tmp) / 'created-set.json').read_text())['update_set_id'], 'b' * 32)
            self.assertFalse((Path(tmp) / 'raw-export.xml').exists())

    def test_polling_is_bounded(self):
        session = FakeSession(state='in progress')
        with tempfile.TemporaryDirectory() as tmp, patch.object(session, 'download') as download:
            with self.assertRaises(m.Rejected):
                m.run(session, Path(tmp), {}, pause=lambda _: None)
            download.assert_not_called()
            self.assertEqual(len(session.calls), 2)

    def test_invalid_xml_cannot_pass(self):
        with tempfile.TemporaryDirectory() as tmp:
            report = {'status': 'failed'}
            with self.assertRaises(Exception):
                m.run(FakeSession(body=b'<html>sentinel-secret</html>'), Path(tmp), report)
            self.assertEqual(report['status'], 'failed')
            self.assertNotIn('sentinel-secret', json.dumps(report))

    def test_main_redacts_exception_and_does_not_reuse_directory(self):
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp) / 'proof'
            env = {'TOPO_XML_PROOF_OUTPUT': str(output), 'SN_SDK_INSTANCE_URL': m.ORIGIN,
                   'SN_SDK_USER': 'admin', 'SN_SDK_USER_PWD': 'sentinel-secret'}
            console = io.StringIO()
            with patch.dict(os.environ, env), patch.object(m.Session, 'login', side_effect=ValueError('sentinel-secret')):
                with contextlib.redirect_stdout(console):
                    self.assertEqual(m.main(), 1)
            self.assertNotIn('sentinel-secret', console.getvalue())
            self.assertNotIn('sentinel-secret', (output / 'status.json').read_text())
            original = (output / 'status.json').read_bytes()
            with patch.dict(os.environ, env), patch.object(m.Session, 'login') as login:
                with contextlib.redirect_stdout(io.StringIO()):
                    self.assertEqual(m.main(), 1)
                login.assert_not_called()
                self.assertEqual((output / 'status.json').read_bytes(), original)


if __name__ == '__main__':
    unittest.main()
