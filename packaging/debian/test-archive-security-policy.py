#!/usr/bin/env python3
"""Check that archive exceptions cannot hide tool failures or other findings."""
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('policy', Path(__file__).parent/'ci/archive_security_policy.py')
policy = importlib.util.module_from_spec(spec)
spec.loader.exec_module(policy)


class ArchivePolicyTests(unittest.TestCase):
    def test_official_snapshot_identity(self):
        self.assertEqual(policy.debian_upstream_version('0.0~git20221128.754e69321358-2', 'github.com/Azure/go-ntlmssp'), 'v0.0.0-20221128193559-754e69321358')
        with self.assertRaises(ValueError):
            policy.debian_upstream_version('0.0~git20221128.aaaaaaaaaaaa-2', 'github.com/Azure/go-ntlmssp')

    def test_regular_debian_revision_and_epoch(self):
        self.assertEqual(policy.debian_upstream_version('1:0.57.0-1'), 'v0.57.0')
        self.assertEqual(policy.debian_upstream_version('1.0.2+really1.0.0-2'), 'v1.0.0')

    def test_clean_scan(self):
        self.assertEqual(policy.classify_scan(0, 'Your code is affected by 0 vulnerabilities.', {})['status'], 'passed')

    def test_known_findings_remain_findings(self):
        actual = {module: version for module, version in policy.ACCEPTED_FINDINGS.values()}
        text = '\n'.join(f'Vulnerability #{i}: {identifier}' for i, identifier in enumerate(policy.ACCEPTED_FINDINGS, 1))
        result = policy.classify_scan(3, text+'\nYour code is affected by 2 vulnerabilities.', actual)
        self.assertEqual(result['status'], 'findings_accepted')
        self.assertEqual(result['govulncheck_exit'], 3)
        self.assertEqual(len(result['reachable_findings']), 2)

    def test_tool_failure_cannot_be_accepted(self):
        with self.assertRaises(ValueError):
            policy.classify_scan(1, 'download failed', {})

    def test_new_finding_or_different_source_cannot_be_accepted(self):
        for identifier, actual in [('GO-2099-9999', {}), ('GO-2024-2918', {'github.com/Azure/azure-sdk-for-go/sdk/azidentity': 'v1.4.0'})]:
            with self.assertRaises(ValueError):
                policy.classify_scan(3, f'Vulnerability #1: {identifier}\nYour code is affected by 1 vulnerability.', actual)

    def test_incomplete_or_inconsistent_report_cannot_pass(self):
        for exit_code, text in [(0, ''), (3, 'Your code is affected by 2 vulnerabilities.'), (0, 'Vulnerability #1: GO-2024-2918\nYour code is affected by 1 vulnerability.')]:
            with self.assertRaises(ValueError):
                policy.classify_scan(exit_code, text, {})


if __name__ == '__main__':
    unittest.main()
