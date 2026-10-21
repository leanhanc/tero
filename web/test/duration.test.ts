import { describe, expect, it } from 'vitest';

import { formatDuration } from '../src/lib/utils/duration';

describe('formatDuration', () => {
	it.each([
		[0, '0s'],
		[-5_000, '0s'],
		[45_900, '45s'],
		[192_000, '3m 12s'],
		[3_900_000, '1h 05m'],
	])('formats %d ms as %j', (milliseconds, text) => {
		expect(formatDuration(milliseconds)).toBe(text);
	});
});
