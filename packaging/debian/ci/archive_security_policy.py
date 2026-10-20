"""Report the owner's scoped exceptions for official Debian archive sources.

These exceptions apply only to this downstream review candidate. They do not
change Topo's upstream security/release gate or mark a vulnerability fixed.
"""
import re

ACCEPTED_FINDINGS = {
    'GO-2024-2918': ('github.com/Azure/azure-sdk-for-go/sdk/azidentity', 'v1.5.0-beta.3'),
    'GO-2026-5543': ('github.com/Azure/go-ntlmssp', 'v0.0.0-20221128193559-754e69321358'),
}


def debian_upstream_version(version, module=''):
    value = version.split(':')[-1]
    upstream = value.rsplit('-', 1)[0] if '-' in value else value
    # This archive source revision names an exact upstream commit; do not turn
    # an arbitrary Debian git snapshot into a made-up semantic version.
    if module == 'github.com/Azure/go-ntlmssp' and upstream == '0.0~git20221128.754e69321358':
        return 'v0.0.0-20221128193559-754e69321358'
    if upstream.count('+really') > 1 or '~' in upstream:
        raise ValueError('Ambiguous actual Debian version: ' + version)
    if '+really' in upstream:
        upstream = upstream.split('+really', 1)[1]
    match = re.match(r'(\d+\.\d+\.\d+)(?:[+]|$)', upstream)
    if not match:
        raise ValueError('Unresolved actual Debian version: ' + version)
    return 'v' + match[1]


def classify_scan(exit_code, output, actual_versions):
    if exit_code not in (0, 3):
        raise ValueError('Vulnerability analyzer failed: exit ' + str(exit_code))
    identifiers = sorted(set(re.findall(r'^Vulnerability #\d+: (GO-\d{4}-\d+)\s*$', output, re.M)))
    total = re.search(r'Your code is affected by (\d+) vulnerabilit(?:y|ies)', output)
    if total is None or int(total[1]) != len(identifiers):
        raise ValueError('Incomplete or inconsistent vulnerability report')
    if (exit_code == 0) != (len(identifiers) == 0):
        raise ValueError('Vulnerability findings do not match analyzer exit')
    for identifier in identifiers:
        if identifier not in ACCEPTED_FINDINGS:
            raise ValueError('New finding requires review: ' + identifier)
        module, version = ACCEPTED_FINDINGS[identifier]
        if actual_versions.get(module) != version:
            raise ValueError('Accepted finding source identity changed: ' + identifier)
    return {
        'status': 'findings_accepted' if identifiers else 'passed',
        'govulncheck_exit': exit_code,
        'reachable_findings': identifiers,
        'accepted_by_owner_on': '2026-10-18' if identifiers else None,
        'policy': 'official Debian library candidate; scoped known findings retained, not fixed',
    }
