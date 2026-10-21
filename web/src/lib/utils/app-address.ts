// The server checks the same rule: 1 to 30 lowercase letters, digits and
// hyphens, starting with a letter and not ending with a hyphen.
const maxAddressLabelLength = 30;
const disallowedCharacters = /[^a-z0-9-]+/g;

/**
 * Turns a display name into the label an app's address starts with, such as
 * "My Shop API" into "my-shop-api". Returns an empty string when nothing
 * usable is left.
 */
export function toAddressLabel(displayName: string): string {
	const lowered = displayName.toLowerCase();
	const withoutAccents = lowered.normalize('NFD').replace(/\p{Diacritic}/gu, '');
	const hyphenated = withoutAccents.replace(disallowedCharacters, '-');
	const trimmed = hyphenated.replace(/^-+|-+$/g, '');
	if (trimmed === '') {
		return '';
	}

	const startsWithLetter = /^[a-z]/.test(trimmed);
	const prefixed = startsWithLetter ? trimmed : `app-${trimmed}`;

	return prefixed.slice(0, maxAddressLabelLength).replace(/-+$/, '');
}
