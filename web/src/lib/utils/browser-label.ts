// The session list names each browser from its user agent. Only the common
// browsers and systems are named; anything else shows the user agent as is.
// Order matters: Edge and Opera also say Chrome, and Chrome also says Safari.
const browsers: [pattern: RegExp, name: string][] = [
	[/Edg\//, 'Edge'],
	[/OPR\//, 'Opera'],
	[/Firefox\//, 'Firefox'],
	[/Chrome\//, 'Chrome'],
	[/Safari\//, 'Safari'],
];

const systems: [pattern: RegExp, name: string][] = [
	[/iPhone|iPad/, 'iOS'],
	[/Android/, 'Android'],
	[/Mac OS X|Macintosh/, 'macOS'],
	[/Windows/, 'Windows'],
	[/CrOS/, 'ChromeOS'],
	[/Linux/, 'Linux'],
];

/** The browser and system a user agent names, or nothing when it names neither. */
export function describeUserAgent(userAgent: string): { browser: string; system: string } | null {
	const browser = browsers.find(([pattern]) => pattern.test(userAgent))?.[1];
	const system = systems.find(([pattern]) => pattern.test(userAgent))?.[1];
	if (!browser || !system) {
		return null;
	}

	return { browser, system };
}
