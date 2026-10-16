#!/usr/bin/env python3
"""Destructive ONLY on fresh hosted Ubuntu or offline Fedora CI; synthetic loopback only.

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


def guard(package_kind):
    require(platform.system() == 'Linux' and os.geteuid() == 0, 'requires root Linux CI')
    require(os.environ.get('GITHUB_ACTIONS') == 'true'
            and os.environ.get('RUNNER_ENVIRONMENT') == 'github-hosted'
            and os.environ.get('RUNNER_OS') == 'Linux'
            and os.environ.get('GITHUB_JOB') == ('fedora-systemd' if package_kind == 'rpm' else 'worker-setup-systemd'), 'wrong CI host/job')
    distro = Path('/etc/os-release').read_text().split('\n')
    require(('ID=fedora' if package_kind == 'rpm' else 'ID=ubuntu') in distro,
            'requires matching disposable distribution')
    if package_kind == 'rpm':
        require(Path('/.dockerenv').exists() and {p.name for p in Path('/sys/class/net').iterdir()} == {'lo'},
                'Fedora fixture requires an offline disposable container')
    require(Path('/proc/1/comm').read_text().strip() == 'systemd'
            and Path('/run/systemd/system').is_dir(), 'real systemd required')
    require(os.environ.get('TOPO_SETUP_DISPOSABLE_CI') == 'yes', 'explicit fixture opt-in required')
    for path in (['/usr/bin/topo', '/usr/local/bin/topo', '/etc/topo-worker',
                 '/usr/lib/systemd/system/topo-worker.service',
                 '/lib/systemd/system/topo-worker.service',
                 '/etc/systemd/system/topo-worker.service',
                 '/etc/systemd/system/topo-worker.service.d',
                 '/usr/lib/systemd/system/topo-worker.service.d',
                 '/lib/systemd/system/topo-worker.service.d',
                 '/run/systemd/system/topo-worker.service.d',
                 '/run/systemd/system/topo-worker.service',
                 '/run/topo-worker-setup.lock',
                 '/usr/local/share/ca-certificates/topo-setup-ci.crt',
                 '/etc/systemd/system/multi-user.target.wants/topo-worker.service',
                 '/etc/pki/ca-trust/source/anchors/topo-setup-ci.crt'] +
                (['/usr/lib/systemd/system/topo-agent.service', '/etc/topo-agent',
                  '/var/lib/topo-agent'] if package_kind == 'rpm' else [])):
        require(not os.path.lexists(path), 'existing Topo path must not be overwritten')
    require(not list(Path('/etc').glob('.topo-worker-setup-*')), 'existing setup stage preserved')
    try:
        grp.getgrnam('topo-setup-ci-other')
    except KeyError:
        pass
    else:
        raise RuntimeError('existing mismatch-test group preserved')
    for name in ['topo-worker'] + (['topo-agent'] if package_kind == 'rpm' else []):
        for lookup in [pwd.getpwnam, grp.getgrnam]:
            try:
                lookup(name)
            except KeyError:
                continue
            raise RuntimeError('existing Topo account/group preserved')
    require(command('systemctl', 'show', 'topo-worker.service', '-p', 'LoadState', ok=False)
            .stdout.strip() == b'LoadState=not-found', 'existing loaded unit preserved')


def snapshot():
    directory = Path('/etc/topo-worker')
    return {p.name: (p.stat().st_uid, p.stat().st_gid, stat.S_IMODE(p.stat().st_mode),
                     hashlib.sha256(p.read_bytes()).hexdigest()) for p in directory.iterdir()}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    packages = parser.add_mutually_exclusive_group(required=True)
    packages.add_argument('--deb')
    packages.add_argument('--rpm')
    parser.add_argument('--upgrade-rpm')
    parser.add_argument('--simulator', required=True)
    args = parser.parse_args()
    package_kind = 'rpm' if args.rpm else 'deb'
    require(bool(args.upgrade_rpm) == bool(args.rpm), 'RPM fixture requires upgrade candidate')
    guard(package_kind)  # Before any mutation, process start, token creation or certificate trust.
    root = Path(tempfile.mkdtemp(prefix='topo-setup-ci-', dir='/var/tmp'))
    os.chmod(root, 0o700)
    made = set()
    server = None
    token = b''
    checks = []
    config = Path('/etc/topo-worker')
    unit = Path('/usr/lib/systemd/system/topo-worker.service')
    cert = Path('/etc/pki/ca-trust/source/anchors/topo-setup-ci.crt' if args.rpm
                else '/usr/local/share/ca-certificates/topo-setup-ci.crt')
    trust_command = ('update-ca-trust', 'extract') if args.rpm else ('update-ca-certificates',)
    try:
        if args.rpm:
            for candidate in [args.rpm, args.upgrade_rpm]:
                require(command('rpm', '-qp', '--qf', '%{NAME}', str(Path(candidate).resolve())).stdout == b'topo',
                        'unexpected candidate package')
            installed = command('rpm', '-Uvh', str(Path(args.rpm).resolve()), ok=False)
            require(installed.returncode == 0, 'RPM installation failed: ' +
                    (installed.stdout + installed.stderr).decode(errors='replace')[:4096])
            made.update({'rpm', 'binary', 'unit', 'user', 'group', 'agent-user', 'agent-group'})
            require(unit.read_bytes() == Path('packaging/systemd/topo-worker.package.service').read_bytes(),
                    'RPM unit differs from reviewed source')
        else:
            extracted = root / 'deb'
            command('dpkg-deb', '--extract', str(Path(args.deb).resolve()), str(extracted))
            binary = extracted / 'usr/bin/topo'
            packaged_unit = extracted / 'usr/lib/systemd/system/topo-worker.service'
            require(binary.is_file() and packaged_unit.is_file(), 'missing package contract')
            require(packaged_unit.read_bytes() == Path('packaging/systemd/topo-worker.package.service').read_bytes(),
                    'packaged unit differs from source')
            command('groupadd', '--system', 'topo-worker'); made.add('group')
            command('useradd', '--system', '--gid', 'topo-worker', '--home-dir', '/nonexistent',
                    '--shell', '/usr/sbin/nologin', '--comment', 'Topo ServiceNow Worker', 'topo-worker')
            made.add('user')
            command('install', '-o', 'root', '-g', 'root', '-m', '0755', str(binary), '/usr/bin/topo')
            made.add('binary')
            command('install', '-o', 'root', '-g', 'root', '-m', '0644', str(packaged_unit), str(unit))
            made.add('unit')
        identity = pwd.getpwnam('topo-worker')
        service_gid = grp.getgrnam('topo-worker').gr_gid
        require(identity.pw_uid != 0 and identity.pw_dir == '/nonexistent'
                and Path(identity.pw_shell).resolve() == Path('/usr/sbin/nologin').resolve(), 'incorrect package identity')
        # Only this fixture's account is altered. The unit's explicit group must win.
        command('groupadd', '--system', 'topo-setup-ci-other'); made.add('other-group')
        command('usermod', '--gid', 'topo-setup-ci-other', 'topo-worker')
        require(pwd.getpwnam('topo-worker').pw_gid != service_gid, 'mismatch fixture not established')
        command('systemctl', 'daemon-reload')
        command('systemd-analyze', 'verify', str(unit))
        require(command('systemctl', 'is-enabled', 'topo-worker.service', ok=False).stdout.strip() == b'disabled',
                'package must begin disabled')
        fixtures = root / 'inputs'; fixtures.mkdir(mode=0o700)
        server = subprocess.Popen([str(Path(args.simulator).resolve()), '-dir', str(fixtures)],
                                  stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        deadline = time.monotonic() + 15
        while not (fixtures / 'origin').exists():
            require(server.poll() is None and time.monotonic() < deadline, 'simulator startup failed')
            time.sleep(0.05)
        token = (fixtures / 'token').read_bytes()
        origin = (fixtures / 'origin').read_text()
        require(origin.startswith('https://127.0.0.1:'), 'non-loopback simulator forbidden')
        command('install', '-m', '0644', str(fixtures / 'ca.crt'), str(cert)); made.add('cert')
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
            master, slave = pty.openpty()
            attrs = termios.tcgetattr(slave); attrs[3] &= ~termios.ECHO
            termios.tcsetattr(slave, termios.TCSANOW, attrs)
            process = subprocess.Popen(['/usr/bin/topo', 'worker', 'setup'], stdin=slave,
                                       stdout=slave, stderr=slave, start_new_session=True)
            os.close(slave)
            body = bytearray(); next_answer = 0; end = time.monotonic() + 90
            try:
                chunk = b""
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
                        prompt, answer = answers[next_answer]
                        if prompt.encode() in body:
                            os.write(master, answer.encode() + b'\n'); next_answer += 1
                    if process.poll() is not None and not select.select([master], [], [], 0)[0]:
                        break
                    if process.poll() is not None and not chunk:
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
                process.wait(timeout=5); os.close(master)
            require(not list(Path('/etc').glob('.topo-worker-setup-*')), 'setup staging file leak')

        def fresh(pool):
            return [('ServiceNow HTTPS instance origin:', origin), ('Existing worker pool ID:', pool),
                    ('Existing site ID:', 'ci-site'), ('OAuth token reference', 'file:' + str(fixtures / 'token')),
                    ('Approved IPv4 CIDR allowlist file', str(fixtures / 'targets.allow')),
                    ('Independently verified SSH known_hosts file', str(fixtures / 'known_hosts')),
                    ('Type verified:', 'verified'), ('Write this new local configuration? Type yes:', 'yes')]

        interact(fresh('ci-typo'), 1, 'initial worker preflight failed')
        require(not config.exists(), 'failed initial preflight published configuration')
        require(command('systemctl', 'is-enabled', 'topo-worker.service', ok=False).stdout.strip() == b'disabled'
                and command('systemctl', 'is-active', 'topo-worker.service', ok=False).stdout.strip() == b'inactive',
                'failed preflight activated service')
        checks.append('mistyped pool rejected before publication; stage removed')
        interact(fresh('ci-setup') + [('Enable and start topo-worker.service now? Type yes:', 'no')],
                 0, 'Configuration saved; service not started')
        before = snapshot()
        require(set(before) == {'setup.json', 'worker-token', 'topo-worker.env', 'targets.allow', 'known_hosts'},
                'unexpected published files')
        require(config.stat().st_uid == 0 and config.stat().st_gid == service_gid
                and stat.S_IMODE(config.stat().st_mode) == 0o750, 'directory ownership/mode')
        for name, (uid, gid, mode, _) in before.items():
            require(uid == 0 and gid == service_gid and mode == (0o600 if name == 'setup.json' else 0o640),
                    'saved ownership/mode')
        require(command('systemctl', 'is-enabled', 'topo-worker.service', ok=False).stdout.strip() == b'disabled'
                and command('systemctl', 'is-active', 'topo-worker.service', ok=False).stdout.strip() == b'inactive',
                'decline activated service')
        checks.append('successful preflight + decline saved protected files, remained disabled/inactive')
        checks.append('mismatched account primary group: files/preflight use explicit named service group')
        interact([('Enable and start topo-worker.service now? Type yes:', 'no')], 0, 'Resuming an intact')
        require(snapshot() == before, 'resume rewrote configuration')
        # Own saved policy tamper must be refused, with all bytes preserved.
        env = config / 'topo-worker.env'; original = env.read_bytes(); env.write_bytes(original + b'# changed\n')
        changed = snapshot()
        interact([], 1, 'existing policy changed')
        require(snapshot() == changed, 'modified existing config not preserved')
        env.write_bytes(original)
        # A missing receipt models an operator-owned, non-setup deployment.
        receipt = config / 'setup.json'; saved_receipt = receipt.read_bytes(); receipt.unlink()
        missing = snapshot()
        interact([], 1, 'existing configuration is not an intact')
        require(snapshot() == missing, 'non-setup configuration changed')
        receipt.write_bytes(saved_receipt); receipt.chmod(0o600); os.chown(receipt, 0, service_gid)
        require(snapshot() == before, 'fixture receipt restoration failed')
        checks.append('safe saved retry and refusal of modified/unreceipted existing deployment')
        require(json.loads((fixtures / 'audit.json').read_text())['counts'].get('claim', 0) == 0,
                'preflight unexpectedly polled for work')
        interact([('Enable and start topo-worker.service now? Type yes:', 'yes')], 0, 'Service started.')
        deadline = time.monotonic() + 15
        while json.loads((fixtures / 'audit.json').read_text())['counts'].get('claim', 0) < 1:
            require(time.monotonic() < deadline, 'running service did not enter simulator claim loop')
            time.sleep(0.05)
        require(command('systemctl', 'is-enabled', 'topo-worker.service').stdout.strip() == b'enabled'
                and command('systemctl', 'is-active', 'topo-worker.service').stdout.strip() == b'active',
                'explicit activation did not enable/start')
        pid = int(command('systemctl', 'show', 'topo-worker.service', '-p', 'MainPID', '--value').stdout)
        status = Path('/proc', str(pid), 'status').read_text()
        require(next(line for line in status.splitlines() if line.startswith('Uid:')).split()[1:] == [str(identity.pw_uid)] * 4,
                'service is not running as packaged user')
        require(next(line for line in status.splitlines() if line.startswith('Gid:')).split()[1:] == [str(service_gid)] * 4,
                'service is not running under explicit named unit group')
        require(snapshot() == before, 'activation rewrote configuration')
        interact([], 1, 'existing service preserved')
        require(snapshot() == before and command('systemctl', 'is-active', 'topo-worker.service').stdout.strip() == b'active',
                'active deployment changed by retry')
        if args.rpm:
            initial_release = command('rpm', '-q', '--qf', '%{RELEASE}', 'topo').stdout
            command('rpm', '-Uvh', str(Path(args.upgrade_rpm).resolve()))
            require(command('rpm', '-q', '--qf', '%{RELEASE}', 'topo').stdout != initial_release,
                    'real RPM version upgrade did not occur')
            require(snapshot() == before and pwd.getpwnam('topo-worker').pw_uid == identity.pw_uid
                    and grp.getgrnam('topo-worker').gr_gid == service_gid, 'upgrade changed protected deployment')
            deadline = time.monotonic() + 35
            while command('systemctl', 'is-active', 'topo-worker.service', ok=False).stdout.strip() != b'active':
                require(time.monotonic() < deadline, 'upgrade did not preserve running service')
                time.sleep(0.1)
            require(command('systemctl', 'is-enabled', 'topo-worker.service').stdout.strip() == b'enabled',
                    'upgrade changed enabled state')
            checks.append('real Fedora RPM upgrade preserved configuration, identity and enabled/running state')
        inspect_argv()
        journal = command('journalctl', '-u', 'topo-worker.service', '--no-pager', '-o', 'cat').stdout
        require(token not in journal, 'token leaked to service journal')
        audit = json.loads((fixtures / 'audit.json').read_text())
        require(audit['workers'] >= 4 and audit['counts'].get('rejected_pool') == 1
                and audit['counts'].get('workers/heartbeat', 0) >= 4, 'missing registration/heartbeat evidence')
        require(all(audit[name] == 0 for name in ['runs', 'tasks', 'deliveries', 'credentials']),
                'simulator offered unexpected discovery work')
        checks.append('saved explicit yes enabled/started exact packaged unit as topo-worker; active retry refused')
        checks.append('no token in PTY output, live process arguments or service journal; no simulator tasks/IRE')
        print(json.dumps({'status': 'passed', 'checks': checks, 'simulator': audit,
                          'scope': 'disposable ' + ('Fedora' if args.rpm else 'Ubuntu') + ' systemd + synthetic loopback; no ServiceNow/target/reboot proof'}))
        if args.rpm:
            command('rpm', '-e', 'topo')
            made.discard('rpm')
            require(snapshot() == before and not unit.exists() and not Path('/usr/bin/topo').exists(),
                    'RPM removal changed private configuration or retained payload')
            require(pwd.getpwnam('topo-worker').pw_uid == identity.pw_uid,
                    'RPM removal deleted preserved service identity')
            print('Fedora RPM install/upgrade/remove lifecycle passed; private configuration and identities preserved')
    finally:
        # Only paths/accounts absent at entry and created by this fixture are removed.
        if 'unit' in made:
            command('systemctl', 'disable', '--now', 'topo-worker.service', ok=False)
        if server is not None:
            server.terminate()
            try:
                server.wait(timeout=5)
            except subprocess.TimeoutExpired:
                server.kill(); server.wait(timeout=5)
        if 'rpm' in made:
            command('rpm', '-e', 'topo', ok=False)
        if 'binary' in made:
            shutil.rmtree(config, ignore_errors=True)
            for stage in Path('/etc').glob('.topo-worker-setup-*'):
                shutil.rmtree(stage)
            Path('/run/topo-worker-setup.lock').unlink(missing_ok=True)
            Path('/usr/bin/topo').unlink(missing_ok=True)
        if 'unit' in made:
            unit.unlink(missing_ok=True); command('systemctl', 'daemon-reload')
            command('systemctl', 'reset-failed', 'topo-worker.service', ok=False)
        if 'cert' in made:
            cert.unlink(); command(*trust_command)
        if 'agent-user' in made:
            command('userdel', 'topo-agent')
        if 'agent-group' in made:
            command('groupdel', 'topo-agent', ok=False)
        if 'user' in made:
            command('userdel', 'topo-worker')
        if 'other-group' in made:
            command('groupdel', 'topo-setup-ci-other', ok=False)
        if 'group' in made:
            command('groupdel', 'topo-worker', ok=False)  # userdel may remove its same-name group.
        shutil.rmtree(root)
        require(not config.exists() and not list(Path('/etc').glob('.topo-worker-setup-*')),
                'task configuration/stage cleanup incomplete')
        require(not unit.exists() and not cert.exists() and not Path('/usr/bin/topo').exists(),
                'task binary/unit/certificate cleanup incomplete')
        for lookup, name in [(pwd.getpwnam, 'topo-worker'), (grp.getgrnam, 'topo-worker'),
                             (grp.getgrnam, 'topo-setup-ci-other')] + ([(pwd.getpwnam, 'topo-agent'), (grp.getgrnam, 'topo-agent')] if args.rpm else []):
            try:
                lookup(name)
            except KeyError:
                continue
            raise RuntimeError('task account/group cleanup incomplete')
        print('Disposable setup fixture cleanup complete')


if __name__ == '__main__':
    main()
