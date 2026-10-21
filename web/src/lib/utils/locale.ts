import { createContext, useContext } from 'react';

import { en, type Messages } from '../../locales/en';
import { es } from '../../locales/es';

// Locales are bundled into the dashboard because the Content-Security-Policy
// allows no requests for them at runtime.
const locales = { en, es } satisfies Record<string, Messages>;

export type Language = keyof typeof locales;

export type Locale = {
	language: Language;
	messages: Messages;
};

const defaultLanguage: Language = 'en';

/**
 * Picks the first of the browser's preferred languages the dashboard has
 * texts for, matching on the primary subtag so es-AR gets Spanish. Falls back
 * to English.
 */
export function detectLanguage(preferredLanguages: readonly string[]): Language {
	for (const tag of preferredLanguages) {
		const primarySubtag = tag.split('-')[0]?.toLowerCase() ?? '';
		if (isLanguage(primarySubtag)) {
			return primarySubtag;
		}
	}

	return defaultLanguage;
}

export function toLocale(language: Language): Locale {
	return { language, messages: locales[language] };
}

function isLanguage(value: string): value is Language {
	return Object.hasOwn(locales, value);
}

export const LocaleContext = createContext<Locale>(toLocale(defaultLanguage));

/** The texts for the dashboard's language. */
export function useMessages(): Messages {
	return useContext(LocaleContext).messages;
}

/** Shows a time in the dashboard's language, such as in the security events. */
export function useFormatTime(): (date: Date) => string {
	const { language } = useContext(LocaleContext);
	return (date) => date.toLocaleString(language);
}
