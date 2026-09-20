#!/usr/bin/env python3
"""Inspect a platform XML export and seal reviewed bytes; never generate XML.

Offline candidate tooling. Synthetic tests do not establish platform compatibility.
A matching review digest records human review, not proof of platform provenance.
"""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import re
import sys
import stat
import xml.etree.ElementTree as ET

SCOPE = 'x_664635_topo'
SCOPE_ID = 'd4e2151fdcbc7d97f8c155d1ba873e46'
MAX_BYTES = 32 * 1024 * 1024
MAX_RECORDS = 4096
# Unknown types require source review, never an automatic allowlist expansion.
ALLOWED = set('''sys_app sys_db_object sys_dictionary sys_documentation sys_choice
sys_index sys_index_column sys_security_acl sys_security_acl_role sys_user_role
sys_user_role_contains sys_app_application sys_app_module sys_script
sys_script_include sysauto_script sys_ui_action sys_ui_action_role
sys_ws_definition sys_ws_version sys_ws_operation sys_scope_privilege sys_module'''.split())


class Rejected(ValueError):
    pass


def require(condition):
    if not condition:
        raise Rejected('XML candidate rejected; inspect privately against the documented contract')


def parse(body):
    require(0 < len(body) <= MAX_BYTES)
    # Restrict encoding before scanning declarations, including nested payloads.
    text = body.decode('utf-8-sig')
    require('\x00' not in text and '<!DOCTYPE' not in text.upper()
            and '<!ENTITY' not in text.upper())
    depth = count = 0
    for event, _ in ET.iterparse(io.StringIO(text), events=('start', 'end')):
        depth += 1 if event == 'start' else -1
        count += event == 'start'
        require(depth <= 32 and count <= 250000)
    root = ET.fromstring(text)
    require(all(isinstance(n.tag, str) and ':' not in n.tag and '{' not in n.tag
                for n in root.iter()))
    return root


def field(record, name):
    nodes = record.findall(name)
    require(len(nodes) == 1 and len(nodes[0]) == 0)
    return (nodes[0].text or '').strip()


def inspect(body):
    root = parse(body)
    require(root.tag == 'unload')
    require(all(n.tag in {'sys_remote_update_set', 'sys_update_xml'} for n in root))
    sets = root.findall('sys_remote_update_set')
    updates = root.findall('sys_update_xml')
    require(len(sets) == 1 and 1 <= len(updates) <= MAX_RECORDS)
    update_set = field(sets[0], 'sys_id')
    require(re.fullmatch('[0-9a-f]{32}', update_set))
    require(field(sets[0], 'state') == 'complete')
    records = []
    seen = set()
    app = None
    tables, roles, routes = [], [], []
    for update in updates:
        require(field(update, 'remote_update_set') == update_set)
        name = field(update, 'name')
        require(re.fullmatch('[A-Za-z0-9_]{1,255}', name) and name not in seen)
        seen.add(name)
        payload = field(update, 'payload').encode('utf-8')
        record_update = parse(payload)
        require(record_update.tag == 'record_update')
        table = record_update.get('table')
        require(table in ALLOWED and name.startswith(table + '_'))
        require(len(record_update) > 0)
        for record in record_update:
            require(record.tag == table and record.get('action') == 'INSERT_OR_UPDATE')
            # No hidden nested record payload or field carrying a credential.
            require(all(len(n) == 0 for n in record))
            require(len({n.tag for n in record}) == len(record))
            require(not any(n.tag.lower() in {'password', 'u_password', 'client_secret',
                                             'access_token', 'refresh_token'} for n in record))
            if table == 'sys_app':
                require(app is None and field(record, 'sys_id') == SCOPE_ID
                        and field(record, 'scope') == SCOPE
                        and field(record, 'name') == 'Nischoy Topo')
                app = field(record, 'version')
                require(re.fullmatch('[0-9]+\.[0-9]+\.[0-9]+', app))
            elif record.find('sys_scope') is not None:
                require(field(record, 'sys_scope') == SCOPE_ID)
            else:
                # Some mapping records omit sys_scope; their references must be
                # examined in the mandatory exact-byte review before packaging.
                require(table in {'sys_security_acl_role', 'sys_user_role_contains',
                                  'sys_ui_action_role', 'sys_index_column'})
            if table == 'sys_db_object':
                tables.append(field(record, 'name'))
            if table == 'sys_user_role':
                roles.append(field(record, 'name'))
            if table == 'sys_ws_operation':
                require(field(record, 'active') == 'true'
                        and field(record, 'requires_authentication') == 'true')
                routes.append(field(record, 'http_method') + ' ' + field(record, 'relative_path'))
            if table == 'sys_scope_privilege':
                require(field(record, 'source_scope') == SCOPE_ID
                        and field(record, 'target_name') == 'sn_cmdb.IdentificationEngine'
                        and field(record, 'operation') == 'execute'
                        and field(record, 'target_type') == 'sys_script_include'
                        and field(record, 'status') == 'allowed')
        records.append({'name': name, 'table': table,
                        'payload_sha256': hashlib.sha256(payload).hexdigest()})
    require(app == '0.4.4')  # Advance only with a reviewed source/upgrade contract.
    require(sorted(tables) == sorted(SCOPE + '_' + name for name in (
        'credential_access', 'credential_binding', 'ire_delivery', 'profile',
        'result', 'run', 'schedule', 'ssh_credential', 'target_scope', 'task',
        'worker', 'worker_pool')))
    require(sorted(roles) == sorted(SCOPE + '.' + name for name in (
        'admin', 'credential_admin', 'operator', 'viewer', 'worker')))
    require(sorted(routes) == sorted('POST ' + path for path in (
        '/claim', '/workers/heartbeat', '/workers/register', '/{id}/complete',
        '/{id}/credential', '/{id}/renew', '/{id}/results')))
    for table, count in {'sys_security_acl': 37, 'sys_security_acl_role': 37,
                         'sys_script_include': 3, 'sys_scope_privilege': 1,
                         'sys_ws_definition': 1, 'sys_ws_version': 1}.items():
        require(sum(r['table'] == table for r in records) == count)
    return {'schema_version': 1, 'scope': SCOPE, 'scope_id': SCOPE_ID,
            'app_version': app, 'update_set_id': update_set,
            'sha256': hashlib.sha256(body).hexdigest(),
            'bytes': len(body), 'records': sorted(records, key=lambda r: r['name']),
            'validation': 'offline-candidate-only; real install/repeat/upgrade evidence required'}


def read_bounded(path):
    # Refuse devices/FIFOs without blocking while opening untrusted local inputs.
    fd = os.open(path, os.O_RDONLY | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_size <= MAX_BYTES)
        return stream.read(MAX_BYTES + 1)


def package(body, report, digest, commit, output):
    require(re.fullmatch('[0-9a-f]{64}', digest) and digest == report['sha256'])
    require(re.fullmatch('[0-9a-f]{40}', commit))
    # New directory only, never overwrite an earlier candidate or release.
    output.mkdir(mode=0o700)
    name = 'nischoy-topo-' + report['app_version'] + '-update-set.xml'
    metadata = dict(report, artifact=name, source_commit=commit,
                    review_sha256=digest, distribution='xml-update-set')
    (output / name).write_bytes(body)
    (output / 'manifest.json').write_text(json.dumps(metadata, indent=2) + '\n')
    manifest_hash = hashlib.sha256((output / 'manifest.json').read_bytes()).hexdigest()
    (output / 'SHA256SUMS').write_text(
        f'{digest}  {name}\n{manifest_hash}  manifest.json\n')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('xml', type=Path)
    parser.add_argument('--output', type=Path)
    parser.add_argument('--review-sha256')
    parser.add_argument('--source-commit')
    args = parser.parse_args()
    try:
        body = read_bounded(args.xml)
        report = inspect(body)
        if args.output:
            require(args.review_sha256 is not None and args.source_commit is not None)
            package(body, report, args.review_sha256, args.source_commit, args.output)
        else:
            require(args.review_sha256 is None and args.source_commit is None)
        print(json.dumps(report, indent=2))
    except (Rejected, ET.ParseError, UnicodeError, OSError):
        # Neither paths nor XML/error snippets may expose imported secrets.
        print('Update-set inspection/packaging failed; no validated release was produced.', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
