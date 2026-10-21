import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { App } from '../src/app';
import { detectLanguage, LocaleContext, toLocale } from '../src/lib/utils/locale';
import type { Messages } from '../src/locales/en';
import { es } from '../src/locales/es';

afterEach(() => {
	cleanup();
	vi.unstubAllGlobals();
});

describe('detectLanguage', () => {
	it.each([
		[['es-AR', 'en'], 'es'],
		[['ES'], 'es'],
		[['fr-FR', 'es-MX', 'en'], 'es'],
		[['en-US', 'es'], 'en'],
		[['fr-FR', 'de'], 'en'],
		[[], 'en'],
	])('picks %j as %j', (preferredLanguages, language) => {
		expect(detectLanguage(preferredLanguages)).toBe(language);
	});
});

describe('Spanish', () => {
	it('shows the login page in Spanish', async () => {
		window.history.replaceState(null, '', '/');
		vi.stubGlobal(
			'fetch',
			vi.fn(async () => new Response(JSON.stringify({ isSetUp: true, isLoggedIn: false }), { status: 200 })),
		);
		render(
			<LocaleContext value={toLocale('es')}>
				<App />
			</LocaleContext>,
		);

		expect(await screen.findByRole('heading', { name: 'Iniciá sesión en Tero' })).toBeTruthy();
		expect(screen.getByLabelText('Contraseña')).toBeTruthy();
		expect(screen.getByRole('button', { name: 'Iniciar sesión' })).toBeTruthy();
	});

	it('fails type checking when a text is missing', () => {
		const { submit: _, ...loginWithoutSubmit } = es.login;
		// @ts-expect-error login.submit is missing.
		const incomplete: Messages = { ...es, login: loginWithoutSubmit };
		expect(incomplete.login).not.toHaveProperty('submit');
	});
});
