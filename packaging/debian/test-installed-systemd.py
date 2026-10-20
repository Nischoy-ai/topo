#!/usr/bin/env python3
# Adapted from the reviewed upstream PTY fixture; adds actual Debian package transactions.
"""Destructive ONLY in this task's fresh hosted Debian systemd container; loopback only.

Uses the built DEB's actual binary and unit, not mocks. No ServiceNow, discovery,
IRE, reboot, production credentials, or workstation execution is permitted.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import pty
import pwd
import grp
import select
import shutil
import signal
import stat
import subprocess
import tempfile
import termios
import time

def require(value, message):
    if not value:
        raise RuntimeError(message)

def command(*args, ok=True):
    result = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=40)
    if ok:
        require(result.returncode == 0, 'bounded fixture command failed: ' + args[0])
    return result

def guard():
    require(platform.system() == 'Linux' and os.geteuid() == 0, 'root Linux CI required')
    require(os.environ.get('GITHUB_ACTIONS') == 'true' and os.environ.get('RUNNER_ENVIRONMENT') == 'github-hosted' and (os.environ.get('GITHUB_JOB') == 'debian-systemd') and (os.environ.get('TOPO_SETUP_DISPOSABLE_CI') == 'yes'), 'wrong disposable CI job')
    require('ID=debian' in Path('/etc/os-release').read_text().splitlines(), 'Debian required')
    require(Path('/.dockerenv').exists() and {p.name for p in Path('/sys/class/net').iterdir()} == {'lo'}, 'offline disposable container required')
    require(Path('/proc/1/comm').read_text().strip() == 'systemd', 'real systemd required')
    for path in ['/usr/bin/topo', '/usr/local/bin/topo', '/etc/topo-worker', '/etc/topo-agent', '/var/lib/topo-agent', '/usr/lib/systemd/system/topo-worker.service', '/usr/lib/systemd/system/topo-agent.service', '/etc/systemd/system/topo-worker.service', '/etc/systemd/system/topo-worker.service.d', '/run/systemd/system/topo-worker.service.d', '/run/topo-worker-setup.lock', '/usr/local/share/ca-certificates/topo-setup-ci.crt']:
        require(not os.path.lexists(path), 'existing Topo path preserved: ' + path)
    require(not list(Path('/etc').glob('.topo-worker-setup-*')), 'existing stage preserved')
    for name in ['topo-worker', 'topo-agent', 'topo-setup-ci-other']:
        for lookup in [pwd.getpwnam, grp.getgrnam]:
            try:
                lookup(name)
            except KeyError:
                continue
            raise RuntimeError('existing account preserved: ' + name)

def snapshot():
    directory = Path('/etc/topo-worker')
    return {p.name: (p.stat().st_uid, p.stat().st_gid, stat.S_IMODE(p.stat().st_mode), hashlib.sha256(p.read_bytes()).hexdigest()) for p in directory.iterdir()}

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--deb', required=True)
    parser.add_argument('--upgrade-deb', required=True)
    parser.add_argument('--simulator', required=True)
    args = parser.parse_args()
    guard()
    root = Path(tempfile.mkdtemp(prefix='topo-setup-ci-', dir='/var/tmp'))
    os.chmod(root, 448)
    made = set()
    server = None
    token = b''
    checks = []
    config = Path('/etc/topo-worker')
    unit = Path('/usr/lib/systemd/system/topo-worker.service')
    cert = Path('/usr/local/share/ca-certificates/topo-setup-ci.crt')
    trust_command = ('update-ca-certificates',)
    try:
        command('dpkg', '--install', str(Path(args.deb).resolve()))
        made.update({'deb', 'binary', 'unit', 'user', 'group', 'agent-user', 'agent-group'})
        require(unit.read_bytes() == Path('packaging/systemd/topo-worker.package.service').read_bytes(), 'unit differs from reviewed source')
        initial_version = command('dpkg-query', '-W', '-f=${Version}', 'topo').stdout
        identity = pwd.getpwnam('topo-worker')
        service_gid = grp.getgrnam('topo-worker').gr_gid
        require(identity.pw_uid != 0 and identity.pw_dir == '/nonexistent' and (Path(identity.pw_shell).resolve() == Path('/usr/sbin/nologin').resolve()), 'incorrect package identity')
        command('groupadd', '--system', 'topo-setup-ci-other')
        made.add('other-group')
        command('usermod', '--gid', 'topo-setup-ci-other', 'topo-worker')
        require(pwd.getpwnam('topo-worker').pw_gid != service_gid, 'mismatch fixture not established')
        command('systemctl', 'daemon-reload')
        command('systemd-analyze', 'verify', str(unit))
        require(command('systemctl', 'is-enabled', 'topo-worker.service', ok=False).stdout.strip() == b'disabled', 'package must begin disabled')
        fixtures = root / 'inputs'
        fixtures.mkdir(mode=448)
        server = subprocess.Popen([str(Path(args.simulator).resolve()), '-dir', str(fixtures)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        deadline = time.monotonic() + 15
        while not (fixtures / 'origin').exists():
            require(server.poll() is None and time.monotonic() < deadline, 'simulator startup failed')
            time.sleep(0.05)
        token = (fixtures / 'token').read_bytes()
        origin = (fixtures / 'origin').read_text()
        require(origin.startswith('https://127.0.0.1:'), 'non-loopback simulator forbidden')
        command('install', '-m', '0644', str(fixtures / 'ca.crt'), str(cert))
        made.add('cert')
        command(*trust_command)

        def inspect_argv():
            for process in Path('/proc').iterdir():
                if not process.name.isdigit():
                    continue
                try:
                    argv = (process / 'cmdline').read_bytes()
                except (FileNotFoundError, PermissionError, ProcessLookupError):
                    continue
                require(token not in argv, 'synthetic token found in process arguments')

        def interact(answers, expected_code, marker):
            (master, slave) = pty.openpty()
            attrs = termios.tcgetattr(slave)
            attrs[3] &= ~termios.ECHO
            termios.tcsetattr(slave, termios.TCSANOW, attrs)
            process = subprocess.Popen(['/usr/bin/topo', 'worker', 'setup'], stdin=slave, stdout=slave, stderr=slave, start_new_session=True)
            os.close(slave)
            body = bytearray()
            next_answer = 0
            end = time.monotonic() + 90
            try:
                chunk = b''
                while True:
                    inspect_argv()
                    require(time.monotonic() < end, 'PTY setup deadline exceeded')
                    if select.select([master], [], [], 0.05)[0]:
                        try:
                            chunk = os.read(master, 4096)
                        except OSError:
                            chunk = b''
                        body.extend(chunk)
                        require(len(body) <= 65536, 'bounded setup transcript exceeded')
                    if next_answer < len(answers):
                        (prompt, answer) = answers[next_answer]
                        if prompt.encode() in body:
                            os.write(master, answer.encode() + b'\n')
                            next_answer += 1
                    if process.poll() is not None and (not select.select([master], [], [], 0)[0]):
                        break
                    if process.poll() is not None and (not chunk):
                        break
                require(token not in body, 'token leaked to setup output')
                require((process.returncode == 0) == (expected_code == 0), 'unexpected setup exit status')
                if marker.encode() not in body or next_answer != len(answers):
                    print('Bounded token-free setup diagnostic:', bytes(body).decode(errors='replace'))
                    print('Answers supplied:', next_answer, 'of', len(answers))
                require(marker.encode() in body and next_answer == len(answers), 'missing setup outcome/prompt')
            finally:
                if process.poll() is None:
                    os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=5)
                os.close(master)
            require(not list(Path('/etc').glob('.topo-worker-setup-*')), 'setup staging file leak')

        def fresh(pool):
            return [('ServiceNow HTTPS instance origin:', origin), ('Existing worker pool ID:', pool), ('Existing site ID:', 'ci-site'), ('OAuth token reference', 'file:' + str(fixtures / 'token')), ('Approved IPv4 CIDR allowlist file', str(fixtures / 'targets.allow')), ('Independently verified SSH known_hosts file', str(fixtures / 'known_hosts')), ('Type verified:', 'verified'), ('Write this new local configuration? Type yes:', 'yes')]
        interact(fresh('ci-typo'), 1, 'initial worker preflight failed')
        require(not config.exists(), 'failed initial preflight published configuration')
        require(command('systemctl', 'is-enabled', 'topo-worker.service', ok=False).stdout.strip() == b'disabled' and command('systemctl', 'is-active', 'topo-worker.service', ok=False).stdout.strip() == b'inactive', 'failed preflight activated service')
        checks.append('mistyped pool rejected before publication; stage removed')
        interact(fresh('ci-setup') + [('Enable and start topo-worker.service now? Type yes:', 'no')], 0, 'Configuration saved; service not started')
        before = snapshot()
        require(set(before) == {'setup.json', 'worker-token', 'topo-worker.env', 'targets.allow', 'known_hosts'}, 'unexpected published files')
        require(config.stat().st_uid == 0 and config.stat().st_gid == service_gid and (stat.S_IMODE(config.stat().st_mode) == 488), 'directory ownership/mode')
        for (name, (uid, gid, mode, _)) in before.items():
            require(uid == 0 and gid == service_gid and (mode == (384 if name == 'setup.json' else 416)), 'saved ownership/mode')
        require(command('systemctl', 'is-enabled', 'topo-worker.service', ok=False).stdout.strip() == b'disabled' and command('systemctl', 'is-active', 'topo-worker.service', ok=False).stdout.strip() == b'inactive', 'decline activated service')
        checks.append('successful preflight + decline saved protected files, remained disabled/inactive')
        checks.append('mismatched account primary group: files/preflight use explicit named service group')
        interact([('Enable and start topo-worker.service now? Type yes:', 'no')], 0, 'Resuming an intact')
        require(snapshot() == before, 'resume rewrote configuration')
        env = config / 'topo-worker.env'
        original = env.read_bytes()
        env.write_bytes(original + b'# changed\n')
        changed = snapshot()
        interact([], 1, 'existing policy changed')
        require(snapshot() == changed, 'modified existing config not preserved')
        env.write_bytes(original)
        receipt = config / 'setup.json'
        saved_receipt = receipt.read_bytes()
        receipt.unlink()
        missing = snapshot()
        interact([], 1, 'existing configuration is not an intact')
        require(snapshot() == missing, 'non-setup configuration changed')
        receipt.write_bytes(saved_receipt)
        receipt.chmod(384)
        os.chown(receipt, 0, service_gid)
        require(snapshot() == before, 'fixture receipt restoration failed')
        checks.append('safe saved retry and refusal of modified/unreceipted existing deployment')
        require(json.loads((fixtures / 'audit.json').read_text())['counts'].get('claim', 0) == 0, 'preflight unexpectedly polled for work')
        interact([('Enable and start topo-worker.service now? Type yes:', 'yes')], 0, 'Service started.')
        deadline = time.monotonic() + 15
        while json.loads((fixtures / 'audit.json').read_text())['counts'].get('claim', 0) < 1:
            require(time.monotonic() < deadline, 'running service did not enter simulator claim loop')
            time.sleep(0.05)
        require(command('systemctl', 'is-enabled', 'topo-worker.service').stdout.strip() == b'enabled' and command('systemctl', 'is-active', 'topo-worker.service').stdout.strip() == b'active', 'explicit activation did not enable/start')
        pid = int(command('systemctl', 'show', 'topo-worker.service', '-p', 'MainPID', '--value').stdout)
        status = Path('/proc', str(pid), 'status').read_text()
        require(next((line for line in status.splitlines() if line.startswith('Uid:'))).split()[1:] == [str(identity.pw_uid)] * 4, 'service is not running as packaged user')
        require(next((line for line in status.splitlines() if line.startswith('Gid:'))).split()[1:] == [str(service_gid)] * 4, 'service is not running under explicit named unit group')
        require(snapshot() == before, 'activation rewrote configuration')
        interact([], 1, 'existing service preserved')
        require(snapshot() == before and command('systemctl', 'is-active', 'topo-worker.service').stdout.strip() == b'active', 'active deployment changed by retry')
        command('dpkg', '--install', str(Path(args.upgrade_deb).resolve()))
        require(command('dpkg-query', '-W', '-f=${Version}', 'topo').stdout != initial_version, 'revision transaction did not occur')
        require(snapshot() == before and pwd.getpwnam('topo-worker').pw_uid == identity.pw_uid and (grp.getgrnam('topo-worker').gr_gid == service_gid), 'revision changed private deployment')
        require(command('systemctl', 'is-active', 'topo-worker.service').stdout.strip() == b'active', 'revision stopped active worker')
        require(command('systemctl', 'is-enabled', 'topo-worker.service').stdout.strip() == b'enabled', 'revision changed enabled state')
        checks.append('same-source Debian package revision preserved configuration, service identity and enabled/running state')
        inspect_argv()
        journal = command('journalctl', '-u', 'topo-worker.service', '--no-pager', '-o', 'cat').stdout
        require(token not in journal, 'token leaked to service journal')
        audit = json.loads((fixtures / 'audit.json').read_text())
        require(audit['workers'] >= 4 and audit['counts'].get('rejected_pool') == 1 and (audit['counts'].get('workers/heartbeat', 0) >= 4), 'missing registration/heartbeat evidence')
        require(all((audit[name] == 0 for name in ['runs', 'tasks', 'deliveries', 'credentials'])), 'simulator offered unexpected discovery work')
        checks.append('saved explicit yes enabled/started exact packaged unit as topo-worker; active retry refused')
        checks.append('no token in PTY output, live process arguments or service journal; no simulator tasks/IRE')
        print(json.dumps({'status': 'passed', 'checks': checks, 'simulator': audit, 'scope': 'disposable ' + 'Debian' + ' systemd + synthetic loopback; no ServiceNow/target/reboot proof'}))
        command('dpkg', '--remove', 'topo')
        deadline = time.monotonic() + 5
        while Path('/proc', str(pid)).exists() and time.monotonic() < deadline:
            time.sleep(0.05)
        require(not Path('/proc', str(pid)).exists(), 'package removal retained the worker process')
        require(snapshot() == before and (not unit.exists()) and (not Path('/usr/bin/topo').exists()), 'removal changed private configuration or retained payload')
        command('dpkg', '--purge', 'topo')
        made.discard('deb')
        require(snapshot() == before and pwd.getpwnam('topo-worker').pw_uid == identity.pw_uid, 'purge deleted operator configuration or service identity')
        print('Debian install/revision/remove/purge passed; operator configuration and identities preserved')
    finally:
        if 'unit' in made:
            command('systemctl', 'disable', '--now', 'topo-worker.service', ok=False)
        if server is not None:
            server.terminate()
            try:
                server.wait(timeout=5)
            except subprocess.TimeoutExpired:
                server.kill()
                server.wait(timeout=5)
        if 'deb' in made:
            command('dpkg', '--purge', 'topo', ok=False)
        if 'binary' in made:
            shutil.rmtree(config, ignore_errors=True)
            for stage in Path('/etc').glob('.topo-worker-setup-*'):
                shutil.rmtree(stage)
            Path('/run/topo-worker-setup.lock').unlink(missing_ok=True)
            Path('/usr/bin/topo').unlink(missing_ok=True)
        if 'unit' in made:
            unit.unlink(missing_ok=True)
            command('systemctl', 'daemon-reload')
            command('systemctl', 'reset-failed', 'topo-worker.service', ok=False)
        if 'cert' in made:
            cert.unlink()
            command(*trust_command)
        if 'agent-user' in made:
            command('userdel', 'topo-agent')
        if 'agent-group' in made:
            command('groupdel', 'topo-agent', ok=False)
        if 'user' in made:
            command('userdel', 'topo-worker')
        if 'other-group' in made:
            command('groupdel', 'topo-setup-ci-other', ok=False)
        if 'group' in made:
            command('groupdel', 'topo-worker', ok=False)
        shutil.rmtree(root)
        require(not config.exists() and (not list(Path('/etc').glob('.topo-worker-setup-*'))), 'task configuration/stage cleanup incomplete')
        require(not unit.exists() and (not cert.exists()) and (not Path('/usr/bin/topo').exists()), 'task binary/unit/certificate cleanup incomplete')
        for (lookup, name) in [(pwd.getpwnam, 'topo-worker'), (grp.getgrnam, 'topo-worker'), (grp.getgrnam, 'topo-setup-ci-other'), (pwd.getpwnam, 'topo-agent'), (grp.getgrnam, 'topo-agent')] + []:
            try:
                lookup(name)
            except KeyError:
                continue
            raise RuntimeError('task account/group cleanup incomplete')
        print('Disposable setup fixture cleanup complete')
if __name__ == '__main__':
    main()
