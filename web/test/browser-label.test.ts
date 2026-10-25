import { describe, expect, it } from 'vitest';

import { describeUserAgent } from '../src/lib/utils/browser-label';

describe('describeUserAgent', () => {
	it.each([
		['Mozilla/5.0 (Macintosh; Intel Mac OS X 14.5; rv:140.0) Gecko/20100101 Firefox/140.0', 'Firefox', 'macOS'],
		[
			'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36 Edg/140.0.0.0',
			'Edge',
			'Windows',
		],
		[
			'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1',
			'Safari',
			'iOS',
		],
		[
			'Mozilla/5.0 (Linux; Android 15) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Mobile Safari/537.36',
			'Chrome',
			'Android',
		],
	])('names %s', (userAgent, browser, system) => {
		expect(describeUserAgent(userAgent)).toEqual({ browser, system });
	});

	it('names nothing it does not know', () => {
		expect(describeUserAgent('curl/8.5.0')).toBeNull();
	});
});
