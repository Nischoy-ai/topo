import importlib.util
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location(
    'coverage', Path(__file__).with_name('check-servicenow-index-coverage.py'))
coverage = importlib.util.module_from_spec(spec)
spec.loader.exec_module(coverage)


class IndexCoverageTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        for table in coverage.xml.TABLES:
            (self.root / (table + '.xml')).write_text(
                '<database><element name="' + table + '" type="collection">'
                '<element name="u_id"/><element name="u_revision"/>'
                '<index name="revision_unique" unique="true">'
                '<element name="u_id"/><element name="u_revision"/>'
                '</index></element></database>')
        self.required = coverage.requirements(self.root)
        self.rows = [{'logical_table_name': r['table'],
                      'index_col_name': ', '.join(r['columns'])} for r in self.required]

    def test_manifest_preserves_order_and_uniqueness(self):
        self.assertEqual(len(self.required), 12)
        self.assertTrue(all(r['unique'] for r in self.required))
        self.assertEqual(self.required[0]['columns'], ['u_id', 'u_revision'])

    def test_coverage_does_not_prove_uniqueness_or_release(self):
        report = coverage.audit(self.required, self.rows)
        self.assertEqual(report['missing_count'], 0)
        self.assertEqual(report['uniqueness'], 'unverified')
        self.assertFalse(report['customer_release'])

    def test_missing_table_and_reversed_columns_rejected(self):
        rows = self.rows[1:]
        rows[0] = dict(rows[0], index_col_name='u_revision, u_id')
        self.assertEqual(coverage.audit(self.required, rows)['missing_count'], 2)

    def test_prefix_index_does_not_satisfy_exact_definition(self):
        self.rows[0]['index_col_name'] = 'u_id'
        self.assertEqual(coverage.audit(self.required, self.rows)['missing_count'], 1)

    def test_empty_snapshot_fails_coverage(self):
        self.assertEqual(coverage.audit(self.required, [])['missing_count'], 12)

    def test_unrelated_or_malformed_rows_rejected(self):
        for row in ({'logical_table_name': 'incident', 'index_col_name': 'number'},
                    {'logical_table_name': self.required[0]['table'], 'index_col_name': 'u_id,...'},
                    {'logical_table_name': self.required[0]['table']}, 'invalid'):
            with self.subTest(row=row), self.assertRaises(ValueError):
                coverage.audit(self.required, [row])

    def test_incomplete_dictionary_rejected(self):
        next(self.root.glob('*.xml')).unlink()
        with self.assertRaises(ValueError):
            coverage.requirements(self.root)

    def test_unknown_unique_and_column_rejected(self):
        path = next(self.root.glob('*.xml'))
        original = path.read_text()
        for bad in (original.replace('unique="true"', 'unique="yes"'),
                    original.replace('<element name="u_revision"/></index>',
                                     '<element name="u_missing"/></index>'),
                    '<!DOCTYPE database>' + original):
            path.write_text(bad)
            with self.assertRaises(ValueError):
                coverage.requirements(self.root)


if __name__ == '__main__':
    unittest.main()
