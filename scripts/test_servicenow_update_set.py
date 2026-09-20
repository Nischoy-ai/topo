"""Synthetic format/security tests; these are not ServiceNow acceptance evidence."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
import xml.etree.ElementTree as ET

spec = importlib.util.spec_from_file_location('updateset', Path(__file__).with_name('package-servicenow-update-set.py'))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


def fixture(table='sys_app', extra=''):
    payload = f'''<record_update table="{table}"><{table} action="INSERT_OR_UPDATE">
<sys_id>{m.SCOPE_ID}</sys_id><name>Nischoy Topo</name><scope>{m.SCOPE}</scope>
<version>0.4.4</version>{extra}</{table}></record_update>'''
    root = ET.Element('unload')
    remote = ET.SubElement(root, 'sys_remote_update_set')
    ET.SubElement(remote, 'sys_id').text = 'a' * 32
    ET.SubElement(remote, 'state').text = 'complete'
    update = ET.SubElement(root, 'sys_update_xml')
    ET.SubElement(update, 'remote_update_set').text = 'a' * 32
    ET.SubElement(update, 'name').text = table + '_' + m.SCOPE_ID
    ET.SubElement(update, 'payload').text = payload
    return ET.tostring(root)


def complete_fixture():
    # Intentionally synthetic identities/content. Tests packaging, not deployability.
    root = ET.fromstring(fixture())
    ordinal = 0

    def add(table, values):
        nonlocal ordinal
        ordinal += 1
        wrapper = ET.Element('record_update', table=table)
        record = ET.SubElement(wrapper, table, action='INSERT_OR_UPDATE')
        for key, value in dict(sys_scope=m.SCOPE_ID, **values).items():
            ET.SubElement(record, key).text = value
        update = ET.SubElement(root, 'sys_update_xml')
        ET.SubElement(update, 'remote_update_set').text = 'a' * 32
        ET.SubElement(update, 'name').text = table + '_' + str(ordinal)
        ET.SubElement(update, 'payload').text = ET.tostring(wrapper, encoding='unicode')

    for name in ('credential_access', 'credential_binding', 'ire_delivery', 'profile',
                 'result', 'run', 'schedule', 'ssh_credential', 'target_scope', 'task',
                 'worker', 'worker_pool'):
        add('sys_db_object', {'name': m.SCOPE + '_' + name})
    for name in ('admin', 'credential_admin', 'operator', 'viewer', 'worker'):
        add('sys_user_role', {'name': m.SCOPE + '.' + name})
    for route in ('/claim', '/workers/heartbeat', '/workers/register', '/{id}/complete',
                  '/{id}/credential', '/{id}/renew', '/{id}/results'):
        add('sys_ws_operation', {'http_method': 'POST', 'relative_path': route,
                                'active': 'true', 'requires_authentication': 'true'})
    for table, count in {'sys_security_acl': 37, 'sys_security_acl_role': 37,
                         'sys_script_include': 3, 'sys_ws_definition': 1,
                         'sys_ws_version': 1}.items():
        for _ in range(count):
            add(table, {})
    add('sys_scope_privilege', {'source_scope': m.SCOPE_ID,
        'target_name': 'sn_cmdb.IdentificationEngine', 'operation': 'execute',
        'target_type': 'sys_script_include', 'status': 'allowed'})
    return ET.tostring(root)


class UpdateSetTests(unittest.TestCase):
    def test_preserves_bytes_and_integrity(self):
        body = complete_fixture()
        report = m.inspect(body)
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp) / 'candidate'
            m.package(body, report, report['sha256'], 'b' * 40, out)
            manifest = json.loads((out / 'manifest.json').read_text())
            self.assertEqual((out / manifest['artifact']).read_bytes(), body)
            self.assertEqual(manifest['distribution'], 'xml-update-set')
            self.assertIn('offline-candidate', manifest['validation'])
            with self.assertRaises(FileExistsError):
                m.package(body, report, report['sha256'], 'b' * 40, out)
            with self.assertRaises(m.Rejected):
                m.package(body, report, '0' * 64, 'b' * 40, Path(tmp) / 'bad')
            self.assertFalse((Path(tmp) / 'bad').exists())

    def test_rejects_operational_and_authorization_records(self):
        for table in ['x_664635_topo_ssh_credential', 'x_664635_topo_task',
                      'sys_user', 'sys_user_has_role', 'oauth_entity',
                      'sys_api_access_policy', 'sys_properties', 'sys_script_fix']:
            with self.subTest(table=table), self.assertRaises(m.Rejected):
                m.inspect(fixture(table))

    def test_rejects_xml_attacks_and_wrong_container(self):
        bad = [b'PK\x03\x04', b'<unload><broken></unload>', b'<record_update/>',
               b'<!DOCTYPE unload [<!ENTITY x "sensitive">]><unload>&x;</unload>',
               ('<a>' * 33 + '</a>' * 33).encode(),
               b'<unload xmlns="foreign"/>', fixture().decode().encode('utf-16'),
               fixture(extra='<password>do-not-log</password>'),
               fixture(extra='<version>9.9.9</version>'),
               fixture(extra='<nested><sys_user/></nested>'),
               fixture().replace(b'complete', b'in progress'),
               fixture().replace(m.SCOPE.encode(), b'foreign'),
               fixture().replace(b'INSERT_OR_UPDATE', b'DELETE')]
        for body in bad:
            with self.subTest(body_length=len(body)), self.assertRaises((m.Rejected, ET.ParseError, UnicodeError)):
                m.inspect(body)

    def test_rejects_duplicate_and_foreign_updates(self):
        root = ET.fromstring(fixture())
        root.append(root.find('sys_update_xml'))
        with self.assertRaises(m.Rejected):
            m.inspect(ET.tostring(root))
        root = ET.fromstring(fixture())
        root.find('sys_update_xml/remote_update_set').text = 'c' * 32
        with self.assertRaises(m.Rejected):
            m.inspect(ET.tostring(root))

    def test_incomplete_or_contract_drift(self):
        for body in [fixture(), complete_fixture().replace(b'POST', b'GET'),
                     complete_fixture().replace(b'/claim', b'/arbitrary'),
                     complete_fixture().replace(b'0.4.4', b'0.4.5')]:
            with self.assertRaises(m.Rejected):
                m.inspect(body)

    def test_nonregular_input_is_rejected(self):
        import os
        with tempfile.TemporaryDirectory() as tmp:
            pipe = Path(tmp) / 'input.xml'
            os.mkfifo(pipe)
            with self.assertRaises(m.Rejected):
                m.read_bounded(pipe)

    def test_error_does_not_echo_payload(self):
        import subprocess
        import sys
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / 'private.xml'
            path.write_bytes(fixture(extra='<password>do-not-log</password>'))
            run = subprocess.run([sys.executable, str(Path(m.__file__)), str(path)],
                                 capture_output=True, text=True)
            self.assertEqual(run.returncode, 1)
            self.assertNotIn('do-not-log', run.stdout + run.stderr)
            self.assertNotIn(str(path), run.stdout + run.stderr)


if __name__ == '__main__':
    unittest.main()
