import assert from 'node:assert/strict';
import { test } from 'node:test';
import { compareKeys, untilEnd } from './gateway.ts';
import { fromRows } from './when.ts';

test('backend keys sort the way the gateway resolves them', () => {
	const sorted = (layout: string, keys: string[]) =>
		keys.toSorted((a, b) => compareKeys(layout, a, b));
	assert.deepEqual(sorted('', ['01.15.2026', '12.31.2025', '02.01.2026']), [
		'12.31.2025',
		'01.15.2026',
		'02.01.2026'
	]);
	assert.deepEqual(sorted('dd.MM.YYYY', ['15.01.2026', '01.02.2026']), [
		'15.01.2026',
		'01.02.2026'
	]);
	assert.deepEqual(sorted('', ['4.100.0', '4.90.0', '4.9.1']), ['4.9.1', '4.90.0', '4.100.0']);
});

test('a bare until day runs through its end', () => {
	assert.equal(untilEnd('2026-10-31'), Date.parse('2026-11-01T00:00:00Z'));
	assert.equal(untilEnd('2026-10-31T12:00:00Z'), Date.parse('2026-10-31T12:00:00Z'));
});

test('when values keep the digits JSON.parse would change', () => {
	const rows = [
		['id', '9007199254740993'],
		['v', '1.10'],
		['store', '2020210'],
		['beta', 'true']
	].map(([fact, value]) => ({ fact, value }));
	assert.deepEqual(fromRows(rows), {
		id: '9007199254740993',
		v: '1.10',
		store: 2020210,
		beta: true
	});
});
