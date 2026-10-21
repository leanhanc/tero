import { describe, expect, it } from 'vitest';

import { toAddressLabel } from '../src/lib/utils/app-address';

describe('toAddressLabel', () => {
	it.each([
		['tero', 'tero'],
		['My Shop API', 'my-shop-api'],
		['My_Cool.API', 'my-cool-api'],
		['Tienda Café', 'tienda-cafe'],
		['2048 game', 'app-2048-game'],
		['___', ''],
		['A very long repository name that goes on', 'a-very-long-repository-name-th'],
	])('turns %j into %j', (displayName, label) => {
		expect(toAddressLabel(displayName)).toBe(label);
	});
});
