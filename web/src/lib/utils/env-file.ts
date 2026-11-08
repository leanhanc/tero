/** One variable read from a pasted .env file. */
export type EnvEntry = { name: string; value: string };

export type ParsedEnvFile = {
	entries: EnvEntry[];
	/** 1-based numbers of the lines that couldn't be read. */
	invalidLines: number[];
};

const assignment = /^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$/;
const doubleQuoteEscapes: Record<string, string> = { n: '\n', r: '\r', t: '\t', '"': '"', '\\': '\\', $: '$' };

/**
 * Reads a .env file in the browser, so nothing leaves it until the admin saves.
 * It understands comments, `export`, quotes and multi-line quoted values. A later
 * line wins over an earlier one of the same name, as when a shell loads the file.
 */
export function parseEnvFile(text: string): ParsedEnvFile {
	const lines = text.replaceAll('\r\n', '\n').split('\n');
	const byName = new Map<string, string>();
	const invalidLines: number[] = [];

	for (let index = 0; index < lines.length; index++) {
		const line = lines[index] ?? '';
		const isSkipped = line.trim() === '' || line.trimStart().startsWith('#');
		if (isSkipped) {
			continue;
		}

		const match = assignment.exec(line);
		const [, name, rawValue] = match ?? [];
		if (name === undefined || rawValue === undefined) {
			invalidLines.push(index + 1);
			continue;
		}

		const read = readValue(rawValue, lines, index);
		if (read === null) {
			invalidLines.push(index + 1);
			continue;
		}

		byName.set(name, read.value);
		index = read.lastLine;
	}

	return { entries: [...byName].map(([name, value]) => ({ name, value })), invalidLines };
}

type ReadValue = { value: string; lastLine: number };

function readValue(rawValue: string, lines: string[], lineIndex: number): ReadValue | null {
	const quote = rawValue[0];
	if (quote === '"' || quote === "'" || quote === '`') {
		return readQuotedValue(quote, rawValue.slice(1), lines, lineIndex);
	}

	if (rawValue.startsWith('#')) {
		return { value: '', lastLine: lineIndex };
	}

	const comment = rawValue.search(/\s#/);
	const unquoted = comment === -1 ? rawValue : rawValue.slice(0, comment);
	return { value: unquoted.trim(), lastLine: lineIndex };
}

/** Reads until the closing quote, across lines. No closing quote is an error. */
function readQuotedValue(quote: string, start: string, lines: string[], lineIndex: number): ReadValue | null {
	let value = '';
	let text = start;
	for (let current = lineIndex; current < lines.length; current++) {
		if (current > lineIndex) {
			text = lines[current] ?? '';
			value += '\n';
		}

		for (let position = 0; position < text.length; position++) {
			const character = text[position] ?? '';
			if (quote === '"' && character === '\\' && position + 1 < text.length) {
				const next = text[position + 1] ?? '';
				value += doubleQuoteEscapes[next] ?? `\\${next}`;
				position++;
				continue;
			}
			if (character === quote) {
				const rest = text.slice(position + 1).trim();
				const isOnlyAComment = rest === '' || rest.startsWith('#');
				return isOnlyAComment ? { value, lastLine: current } : null;
			}
			value += character;
		}
	}

	return null;
}
