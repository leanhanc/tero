import { describe, expect, it } from 'vitest';

import { parseEnvFile } from '../src/lib/utils/env-file';

describe('parseEnvFile', () => {
	it('reads comments, export and quotes', () => {
		const parsed = parseEnvFile(['# a comment', '', 'export A="1"', 'B=2 # trailing', "C='lit\\n'", 'D=', 'E = spaced'].join('\n'));

		expect(parsed.invalidLines).toEqual([]);
		expect(Object.fromEntries(parsed.entries.map(({ name, value }) => [name, value]))).toEqual({
			A: '1',
			B: '2',
			C: 'lit\\n',
			D: '',
			E: 'spaced',
		});
	});

	it('keeps a multi-line quoted value', () => {
		const parsed = parseEnvFile('KEY="-----BEGIN KEY-----\nabc=\n-----END KEY-----"\nNEXT=1');

		expect(parsed.entries).toEqual([
			{ name: 'KEY', value: '-----BEGIN KEY-----\nabc=\n-----END KEY-----' },
			{ name: 'NEXT', value: '1' },
		]);
	});

	it('expands escapes in double quotes only', () => {
		const parsed = parseEnvFile('A="x\\ny\\"z"');

		expect(parsed.entries).toEqual([{ name: 'A', value: 'x\ny"z' }]);
	});

	it('reports what it cannot read', () => {
		const parsed = parseEnvFile('not an assignment\nOK=1\n1BAD=2\nOPEN="never closed');

		expect(parsed.entries).toEqual([{ name: 'OK', value: '1' }]);
		expect(parsed.invalidLines).toEqual([1, 3, 4]);
	});

	it('reads a comment after an empty value as no value', () => {
		expect(parseEnvFile('A= # note\nB="x" # note').entries).toEqual([
			{ name: 'A', value: '' },
			{ name: 'B', value: 'x' },
		]);
	});

	it('refuses text after a closing quote', () => {
		expect(parseEnvFile('A="x"y').invalidLines).toEqual([1]);
	});

	it('lets a later line win', () => {
		expect(parseEnvFile('A=1\nA=2').entries).toEqual([{ name: 'A', value: '2' }]);
	});

	it('handles Windows line endings', () => {
		expect(parseEnvFile('A=1\r\nB=2\r\n').entries).toEqual([
			{ name: 'A', value: '1' },
			{ name: 'B', value: '2' },
		]);
	});
});
