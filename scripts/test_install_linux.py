"""Installer safety/dispatch tests; every privileged command is a local fake."""
import os
import shutil
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).with_name('install-linux.sh').resolve()
MOCK = '''#!/usr/bin/python3
import os, pathlib, sys
name=pathlib.Path(sys.argv[0]).name
args=sys.argv[1:]
root=pathlib.Path(os.environ['FIXTURE'])
with (root/'calls').open('a') as f: f.write(name+' '+' '.join(args)+'\\n')
if name=='uname': print(os.environ.get('SYSTEM','Linux') if args==['-s'] else 'x86_64')
elif name=='id': print('0')
elif name=='curl':
 if os.environ.get('FAIL')=='curl': sys.exit(22)
 pathlib.Path(args[args.index('-o')+1]).write_text('key')
elif name=='gpg':
 if '--show-keys' in args:
  print('pub:::::::::')
  print('fpr:::::::::'+os.environ.get('KEY','6049C01BB18CE8EC395DA16F9C64F25B652F0673')+':')
 else: pathlib.Path(args[args.index('--output')+1]).write_text('binary key')
elif name=='install':
 (root/pathlib.Path(args[-1]).name).write_bytes(pathlib.Path(args[-2]).read_bytes())
elif name=='apt-get' and os.environ.get('FAIL')=='apt': sys.exit(100)
elif name=='topo': print('v0.1.0-beta.1')
'''


class InstallerTests(unittest.TestCase):
    def run_installer(self, channel='apt', **changes):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary = root/'bin'
            binary.mkdir()
            for tool in ['uname', 'id', 'curl', 'gpg', 'install', 'topo'] + (
                    ['apt-get'] if channel == 'apt' else ['dnf', 'rpm']):
                p = binary/tool
                p.write_text(MOCK)
                p.chmod(0o755)
            for tool in ['awk', 'mktemp', 'rm', 'cat']:
                (binary/tool).symlink_to(shutil.which(tool))
            env = dict(os.environ, PATH=str(binary), FIXTURE=str(root), **changes)
            result = subprocess.run(['/bin/sh', str(SCRIPT)], env=env,
                                    capture_output=True, text=True)
            files = {p.name: p.read_text() for p in root.iterdir() if p.is_file()}
            return result, files

    def test_apt_signed_repository(self):
        result, files = self.run_installer()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('Signed-By: /etc/apt/keyrings/nischoy-topo.gpg', files['nischoy-topo.sources'])
        self.assertIn('Suites: beta', files['nischoy-topo.sources'])
        self.assertIn('install -y topo', files['calls'])
        self.assertNotIn('systemctl', files['calls'])

    def test_rpm_signed_repository(self):
        result, files = self.run_installer('rpm')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('repo_gpgcheck=1', files['nischoy-topo.repo'])
        self.assertIn('gpgcheck=1', files['nischoy-topo.repo'])
        self.assertIn('/rpm/beta/$basearch', files['nischoy-topo.repo'])
        self.assertIn('dnf install -y topo', files['calls'])

    def test_untrusted_key_and_download_failure_do_not_mutate(self):
        for changes in [{'KEY': 'BAD'}, {'KEY': '6049C01BB18CE8EC395DA16F9C64F25B652F0673\npub:::::::::\nfpr:::::::::BAD'}, {'FAIL': 'curl'}]:
            with self.subTest(changes=changes):
                result, files = self.run_installer(**changes)
                self.assertNotEqual(result.returncode, 0)
                self.assertNotIn('\ninstall ', files['calls'])
                self.assertNotIn('\napt-get ', files['calls'])

    def test_package_failure_does_not_report_success(self):
        result, files = self.run_installer(FAIL='apt')
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('Topo installed', result.stdout)
        self.assertNotIn('topo version', files['calls'])

    def test_non_linux_rejected(self):
        result, files = self.run_installer(SYSTEM='Darwin')
        self.assertEqual(result.returncode, 2)
        self.assertNotIn('curl ', files['calls'])


if __name__ == '__main__':
    unittest.main()
