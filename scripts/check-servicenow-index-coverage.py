#!/usr/bin/env python3
"""Derive required indexes from a clean SDK build; audit physical column coverage.

The v_index_creator view does not expose uniqueness. Coverage is necessary but
never sufficient for release approval. No network, credentials, or mutations.
"""
import argparse
import importlib.util
import json
from pathlib import Path
import re
import sys

spec = importlib.util.spec_from_file_location(
    'xml_package', Path(__file__).with_name('package-servicenow-update-set.py'))
xml = importlib.util.module_from_spec(spec)
spec.loader.exec_module(xml)


def identifier(value):
    xml.require(isinstance(value, str) and re.fullmatch(r'[a-z][a-z0-9_]{0,127}', value))
    return value


def requirements(directory):
    files = sorted(directory.glob('*.xml'))
    xml.require(len(files) == len(xml.TABLES))
    tables, result, size = set(), [], 0
    for path in files:
        body = xml.read_bounded(path)
        size += len(body)
        xml.require(size <= xml.MAX_BYTES)
        root = xml.parse(body)
        xml.require(root.tag == 'database' and len(root) == 1)
        table = root[0]
        name = table.get('name')
        xml.require(table.tag == 'element' and table.get('type') == 'collection'
                    and name in xml.TABLES and name not in tables)
        tables.add(name)
        fields = {identifier(n.get('name')) for n in table.findall('element')}
        fields.update(('sys_id', 'sys_created_on', 'sys_updated_on'))
        seen = set()
        indexes = table.findall('index')
        xml.require(1 <= len(indexes) <= 64)
        for index in indexes:
            index_name = identifier(index.get('name'))
            xml.require(set(index.attrib) == {'name', 'unique'}
                        and index.get('unique') in {'true', 'false'})
            columns = []
            for column in index:
                xml.require(column.tag == 'element' and set(column.attrib) == {'name'}
                            and len(column) == 0 and column.get('name') in fields)
                columns.append(identifier(column.get('name')))
            xml.require(1 <= len(columns) <= 16 and len(columns) == len(set(columns))
                        and index_name not in seen)
            seen.add(index_name)
            result.append({'table': name, 'name': index_name, 'columns': columns,
                           'unique': index.get('unique') == 'true'})
    xml.require(tables == xml.TABLES)
    return sorted(result, key=lambda r: (r['table'], r['name']))


def audit(required, observed):
    xml.require(isinstance(observed, list) and len(observed) <= 4096)
    present = set()
    for row in observed:
        xml.require(isinstance(row, dict))
        table = row.get('logical_table_name')
        raw = row.get('index_col_name')
        xml.require(table in xml.TABLES and isinstance(raw, str) and len(raw) <= 2048)
        columns = tuple(identifier(c.strip()) for c in raw.split(','))
        xml.require(1 <= len(columns) <= 16)
        present.add((table, columns))
    missing = [r for r in required if (r['table'], tuple(r['columns'])) not in present]
    return {'required_count': len(required),
            'required_unique_count': sum(r['unique'] for r in required),
            'missing_count': len(missing), 'missing': missing,
            'column_coverage': 'missing' if missing else 'present',
            'uniqueness': 'unverified', 'source_equivalence': 'not_checked',
            'customer_release': False}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--dictionary', required=True, type=Path)
    parser.add_argument('--observed', type=Path,
                        help='JSON array of v_index_creator rows, queried by exact table ID')
    args = parser.parse_args()
    try:
        required = requirements(args.dictionary)
        result = {'required': required, 'customer_release': False}
        if args.observed:
            result = audit(required, json.loads(xml.read_bounded(args.observed)))
        print(json.dumps(result, indent=2, sort_keys=True))
        return 1 if result.get('missing_count') else 0
    except (ValueError, OSError, xml.ET.ParseError):
        print('Index coverage input rejected; inspect privately.', file=sys.stderr)
        return 2


if __name__ == '__main__':
    sys.exit(main())
