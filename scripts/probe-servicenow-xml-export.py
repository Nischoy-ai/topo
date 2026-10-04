#!/usr/bin/env python3
"""Bounded native-UI export proof; not a supported REST API or release publisher.

Credentials come only from environment variables. Raw exports stay local and
are never uploaded by the accompanying workflow. All errors are payload-free.
"""
import hashlib
import http.cookiejar
import importlib.util
import json
import os
from pathlib import Path
import re
import time
import urllib.error
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET

APP = 'd4e2151fdcbc7d97f8c155d1ba873e46'
SCOPE = 'x_664635_topo'
ORIGIN = 'https://dev394887.service-now.com'
EXPORT_ACTION = 'fb1a56050a0a3c1e01f8b4066aff9aa7'
MAX_BYTES = 32 * 1024 * 1024


class Rejected(Exception):
    pass


def require(condition):
    if not condition:
        raise Rejected()


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class Session:
    def __init__(self, origin):
        # This proof intentionally cannot point at the acceptance/customer instance.
        require(origin == ORIGIN)
        self.origin = origin
        self.deadline = time.monotonic() + 300
        self.token = ''
        self.opener = urllib.request.build_opener(
            NoRedirect(), urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def request(self, path, data=None, statuses=(200,)):
        require(path.startswith('/') and not path.startswith('//'))
        url = urllib.parse.urlsplit(self.origin + path)
        require(url.scheme == 'https' and url.netloc == urllib.parse.urlsplit(self.origin).netloc
                and not url.fragment and not url.username and not url.password)
        remaining = self.deadline - time.monotonic()
        require(remaining > 0)
        headers = {'Accept': '*/*'}
        if self.token:
            headers['X-UserToken'] = self.token
        if data is not None:
            headers['Content-Type'] = 'application/x-www-form-urlencoded'
            data = urllib.parse.urlencode(data).encode()
        request = urllib.request.Request(self.origin + path, data=data, headers=headers)
        try:
            response = self.opener.open(request, timeout=min(20, remaining))
        except urllib.error.HTTPError as error:
            response = error
        with response:
            require(response.status in statuses)
            chunks, size = [], 0
            while True:
                require(time.monotonic() < self.deadline)
                chunk = response.read(min(65536, MAX_BYTES + 1 - size))
                if not chunk:
                    break
                size += len(chunk)
                require(size <= MAX_BYTES)
                chunks.append(chunk)
            return response.status, response.headers, b''.join(chunks)

    def login(self, username, password):
        require(username == 'admin' and 0 < len(password) <= 4096)
        # Matches the pinned SDK's UISession password-login flow, including CSRF.
        _, _, body = self.request('/angular.do?sysparm_type=view_form.login', {
            'sysparm_type': 'login', 'ni.nolog.user_password': 'true',
            'user_name': username, 'user_password': password})
        result = json.loads(body)
        require(isinstance(result, dict) and result.get('status') == 'success')
        _, headers, _ = self.request('/angular.do?sysparm_type=get_user', {}, (200, 401))
        token = headers.get('x-usertoken-response', '')
        require(re.fullmatch(r'[A-Za-z0-9_-]{16,256}', token))
        self.token = token

    def rows(self, table, query, fields, limit=2):
        require(table in {'sys_app', 'sys_update_set'})
        params = urllib.parse.urlencode({'sysparm_query': query, 'sysparm_fields': fields,
            'sysparm_limit': str(limit), 'sysparm_display_value': 'false',
            'sysparm_exclude_reference_link': 'true'})
        _, _, body = self.request('/api/now/table/' + table + '?' + params)
        rows = json.loads(body)['result']
        require(isinstance(rows, list) and len(rows) <= limit)
        return rows

    def ajax(self, parameters):
        _, _, body = self.request('/xmlhttp.do', dict(parameters,
            sysparm_processor='com.snc.apps.AppsAjaxProcessor', sysparm_ck=self.token))
        require(len(body) < 1024 * 1024 and b'<!DOCTYPE' not in body.upper()
                and b'<!ENTITY' not in body.upper())
        root = ET.fromstring(body)
        require(root.tag == 'xml')
        return root.get('answer', '')

    def download(self, set_id):
        _, headers, body = self.request('/sys_update_set.do', {
            'sys_id': set_id, 'sys_uniqueValue': set_id, 'sys_target': 'sys_update_set',
            'sys_action': EXPORT_ACTION, 'sysparm_ck': self.token}, (200, 302, 303))
        location = headers.get('Location')
        # The native action redirects to the native download processor. Never
        # follow a login, cross-origin, or unexpected redirect with credentials.
        require(location is not None)
        target = urllib.parse.urlsplit(urllib.parse.urljoin(self.origin, location))
        require(target.scheme == 'https' and target.netloc == urllib.parse.urlsplit(self.origin).netloc
                and target.path == '/export_update_set.do' and not target.fragment
                and not target.username and not target.password)
        query = urllib.parse.parse_qs(target.query, strict_parsing=True)
        require(set(query) <= {'sysparm_sys_id', 'sysparm_delete_when_done',
                              'sysparm_is_remote', 'sysparm_ck'}
                and all(len(v) == 1 for v in query.values())
                and re.fullmatch('[0-9a-f]{32}', query.get('sysparm_sys_id', [''])[0]))
        _, _, body = self.request(target.path + '?' + target.query)
        return body


def inspector():
    spec = importlib.util.spec_from_file_location('xml_inspector',
        Path(__file__).with_name('package-servicenow-update-set.py'))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def save(path, value):
    # New proof directory is private; still refuse accidental file replacement.
    with path.open('x', encoding='utf-8') as stream:
        json.dump(value, stream, indent=2)
        stream.write('\n')
    path.chmod(0o600)


def run(session, output, report, pause=time.sleep):
    app = session.rows('sys_app', 'sys_id=' + APP, 'sys_id,scope,version')
    require(len(app) == 1 and app[0] == {'sys_id': APP, 'scope': SCOPE, 'version': '0.4.6'})
    report['stage'] = 'creating_update_set'
    set_id = session.ajax({'sysparm_function': 'createUpdateSet',
        'sysparm_name': 'Nischoy Topo XML CI proof', 'sysparm_appid': APP,
        'sysparm_description': 'Unattended export proof only; no runtime/demo data; not a release.',
        'sysparm_current': 'false'})
    require(re.fullmatch('[0-9a-f]{32}', set_id))
    report['update_set_id'] = set_id
    # Persist the identity BEFORE starting publication. Failed writes are never retried.
    save(output / 'created-set.json', {'update_set_id': set_id})
    report['stage'] = 'publishing'
    session.ajax({'sysparm_function': 'publishToUpdateSet', 'sysparm_update_set_id': set_id,
        'sysparm_sys_id': APP, 'sysparm_name': 'Nischoy Topo - 0.4.6',
        'sysparm_version': '0.4.6', 'sysparm_description': 'Unattended XML export proof.',
        'sysparm_include_data': 'false', 'sysparm_progress_name': 'Publishing application'})
    for _ in range(40):
        state = session.rows('sys_update_set', 'sys_id=' + set_id,
                             'sys_id,state,application')
        require(len(state) == 1 and state[0].get('sys_id') == set_id
                and state[0].get('application') == APP)
        if state[0].get('state') == 'complete':
            break
        require(state[0].get('state') == 'in progress')
        pause(3)
    else:
        raise Rejected()
    report['stage'] = 'downloading'
    body = session.download(set_id)
    # Raw bytes may contain unexpected data; stay on the ephemeral runner and
    # never become public Actions artifacts, even when inspection fails.
    raw = output / 'raw-export.xml'
    with raw.open('xb') as stream:
        stream.write(body)
    raw.chmod(0o600)
    report['stage'] = 'inspecting'
    checked = inspector().inspect(body)
    require(checked['app_version'] == app[0]['version'])
    report.update(status='passed', stage='complete', app_version=checked['app_version'],
        xml_sha256=hashlib.sha256(body).hexdigest(), xml_bytes=len(body),
        update_count=len(checked['records']), exported_set_id=checked['update_set_id'],
        source_equivalence='not_checked', customer_release=False)


def main():
    report = {'status': 'failed', 'stage': 'configuration', 'customer_release': False}
    output = None
    created = False
    try:
        output = Path(os.environ['TOPO_XML_PROOF_OUTPUT'])
        output.mkdir(mode=0o700)  # No overwrite or reuse of another run.
        created = True
        session = Session(os.environ['SN_SDK_INSTANCE_URL'])
        username = os.environ.pop('SN_SDK_USER')
        password = os.environ.pop('SN_SDK_USER_PWD')
        report['stage'] = 'login'
        session.login(username, password)
        del password
        report['stage'] = 'app_preflight'
        run(session, output, report)
    except Exception:
        # Never print HTTP bodies, credentials, cookies, redirect URLs or errors.
        print('XML export proof failed; inspect the payload-free status report.')
    finally:
        if created:
            try:
                save(output / 'status.json', report)
            except OSError:
                report['status'] = 'failed'
    if report['status'] == 'passed':
        print('XML export proof passed; publication remains disabled.')
        return 0
    return 1


if __name__ == '__main__':
    raise SystemExit(main())
