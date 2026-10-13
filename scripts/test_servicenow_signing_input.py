"""Offline adversarial signing-input tests; no publication or acceptance claim."""
import hashlib
import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('signing_input',
    Path(__file__).with_name('verify-servicenow-signing-input.py'))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class SigningInputTests(unittest.TestCase):
    def fixture(self, directory, files=None):
        lines = []
        for name in sorted(m.FILES if files is None else files):
            body = ('synthetic: ' + name).encode()
            (directory / name).write_bytes(body)
            lines.append(hashlib.sha256(body).hexdigest() + '  ' + name + '\n')
        expected = ''.join(lines).encode()
        (directory / 'SHA256SUMS').write_bytes(expected)
        return expected

    def test_exact_files_pass(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            m.verify(self.fixture(directory), directory)

    def test_changed_manifest_or_payload_rejected(self):
        for name in ['SHA256SUMS', 'nischoy-topo-0.4.6-combined.xml']:
            with self.subTest(name=name), tempfile.TemporaryDirectory() as tmp:
                directory = Path(tmp)
                expected = self.fixture(directory)
                (directory / name).write_bytes(b'tampered')
                with self.assertRaises(ValueError):
                    m.verify(expected, directory)

    def test_missing_extra_and_symlink_files_rejected(self):
        for change in ['missing', 'extra', 'symlink']:
            with self.subTest(change=change), tempfile.TemporaryDirectory() as tmp:
                directory = Path(tmp)
                expected = self.fixture(directory)
                target = directory / 'INDEXES.md'
                if change == 'extra':
                    (directory / 'extra.xml').write_bytes(b'unknown')
                else:
                    target.unlink()
                    if change == 'symlink':
                        target.symlink_to(directory / 'INSTALLATION.md')
                with self.assertRaises((ValueError, FileNotFoundError)):
                    m.verify(expected, directory)

    def test_traversal_duplicate_missing_and_non_ascii_entries_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            expected = self.fixture(directory)
            for bad in [expected.replace(b'INDEXES.md', b'../INDEXES.md'),
                        expected + expected.splitlines(keepends=True)[0],
                        b'\n'.join(expected.splitlines()[1:]),
                        expected + b'\xff\n']:
                (directory / 'SHA256SUMS').write_bytes(bad)
                with self.subTest(bad=bad[:64]), self.assertRaises(ValueError):
                    m.verify(bad, directory)

    def test_aggregate_read_bound(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            expected = self.fixture(directory)
            with patch.object(m, 'MAX_BYTES', 10), self.assertRaises(ValueError):
                m.verify(expected, directory)

    def test_release_specific_filenames_cannot_be_mixed(self):
        files = {name.replace('0.4.6', '0.4.7') for name in m.FILES}
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            expected = self.fixture(directory, files)
            m.verify(expected, directory, files)
            with self.assertRaises(ValueError):
                m.verify(expected, directory)
            mixed = expected.replace(b'nischoy-topo-0.4.7-combined.xml',
                                     b'nischoy-topo-0.4.6-combined.xml')
            with self.assertRaises(ValueError):
                m.parse_manifest(mixed, files)

    def test_only_reviewed_release_tags_select_manifests(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            read_regular = m.read_regular
            root = Path(m.__file__).resolve().parent.parent
            for tag in sorted(m.REVIEWED_RELEASES):
                directory = Path(tmp) / tag
                directory.mkdir()
                expected = self.fixture(directory, m.FILES_BY_RELEASE[tag])
                manifest = root / 'release' / (tag + '.SHA256SUMS')
                def read(path, maximum):
                    return expected if path == manifest else read_regular(path, maximum)
                with self.subTest(tag=tag), patch.object(m.sys, 'argv', ['verify', str(directory), tag]), patch.object(m, 'read_regular', side_effect=read) as calls:
                    self.assertEqual(m.main(), 0)
                    self.assertEqual(calls.call_args_list[0].args, (manifest, 4096))
            for tag in ['../../unknown', '--repo=unknown', 'servicenow-0.4.7', '']:
                with self.subTest(tag=tag), patch.object(m.sys, 'argv', ['verify', str(directory), tag]), patch.object(m, 'read_regular') as read:
                    self.assertEqual(m.main(), 1)
                    read.assert_not_called()

    def test_each_manifest_only_release_uses_its_own_filenames(self):
        for tag in sorted(m.MANIFEST_ONLY_RELEASES):
            with self.subTest(tag=tag), tempfile.TemporaryDirectory() as tmp:
                directory = Path(tmp)
                full = directory / 'full'
                full.mkdir()
                expected = self.fixture(full, m.FILES_BY_RELEASE[tag])
                staged = directory / 'staged'
                staged.mkdir()
                (staged / 'SHA256SUMS').write_bytes(expected)
                manifest = Path(m.__file__).resolve().parent.parent / 'release' / (tag + '.SHA256SUMS')
                read_regular = m.read_regular
                def read(path, maximum):
                    return expected if path == manifest else read_regular(path, maximum)
                with patch.object(m.sys, 'argv', ['verify', str(staged), tag, '--manifest-only']), patch.object(m, 'read_regular', side_effect=read):
                    self.assertEqual(m.main(), 0)
                    (staged / 'SHA256SUMS').write_bytes(expected.replace(b'-combined.xml', b'-other.xml'))
                    self.assertEqual(m.main(), 1)

    def test_manifest_only_mode_is_explicit_and_pins_exact_reviewed_bytes(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            full = root / 'package'
            full.mkdir()
            expected = self.fixture(full)
            staged = root / 'staged'
            staged.mkdir()
            (staged / 'SHA256SUMS').write_bytes(expected)
            read_regular = m.read_regular
            manifest = Path(m.__file__).resolve().parent.parent / 'release/servicenow-0.4.6-beta.2.SHA256SUMS'
            def read(path, maximum):
                return expected if path == manifest else read_regular(path, maximum)
            argv = ['verify', str(staged), 'servicenow-0.4.6-beta.2', '--manifest-only']
            with patch.object(m, 'read_regular', side_effect=read), patch.object(m.sys, 'argv', argv):
                self.assertEqual(m.main(), 0)
                (staged / 'SHA256SUMS').write_bytes(b'tampered')
                self.assertEqual(m.main(), 1)
                (staged / 'SHA256SUMS').write_bytes(expected)
                (staged / 'unexpected').write_bytes(b'unknown')
                self.assertEqual(m.main(), 1)
            for tag, mode in [('servicenow-0.4.6-beta', '--manifest-only'), ('servicenow-0.4.6-beta.2', '--unknown')]:
                with patch.object(m.sys, 'argv', ['verify', str(staged), tag, mode]), patch.object(m, 'read_regular') as calls:
                    self.assertEqual(m.main(), 1)
                    calls.assert_not_called()


if __name__ == '__main__':
    unittest.main()
