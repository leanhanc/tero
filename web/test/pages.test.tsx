import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { App } from '../src/app';

type MockResponse = { status: number; body?: unknown };

/** Answers API calls in order and records what was sent. */
function mockAPI(responses: Record<string, MockResponse[]>) {
	const calls: { path: string; body: unknown }[] = [];
	vi.stubGlobal(
		'fetch',
		vi.fn(async (path: string, init?: RequestInit) => {
			calls.push({ path, body: init?.body ? JSON.parse(String(init.body)) : undefined });
			const next = responses[path]?.shift() ?? { status: 500, body: { error: 'unexpected call' } };
			return new Response(next.body === undefined ? null : JSON.stringify(next.body), { status: next.status });
		}),
	);
	return calls;
}

function openAt(url: string) {
	window.history.replaceState(null, '', url);
}

afterEach(() => {
	cleanup();
	vi.unstubAllGlobals();
});

describe('setup page', () => {
	it('labels the password field for password managers and sends the token from the fragment (v5.0.0-6.2.7)', async () => {
		openAt('/setup#the-token');
		const calls = mockAPI({
			'/api/setup/begin': [{ status: 200, body: { secret: 'ABC', uri: 'otpauth://totp/x', qrCode: 'data:image/png;base64,AA' } }],
		});
		render(<App />);

		const password = screen.getByLabelText('Password');
		expect(password).toHaveProperty('autocomplete', 'new-password');
		expect(password.getAttribute('minlength')).toBe('8');
		expect(password.getAttribute('aria-describedby')).toContain('password-hint');

		await userEvent.type(password, 'a long enough passphrase');
		await userEvent.click(screen.getByRole('button', { name: 'Continue' }));

		const sent = calls.find((call) => call.path === '/api/setup/begin');
		expect(sent?.body).toEqual({ token: 'the-token', password: 'a long enough passphrase' });
		expect(await screen.findByRole('img', { name: /QR code/ })).toBeTruthy();
		expect(screen.getByText('ABC')).toBeTruthy();

		const code = screen.getByLabelText('6-digit code from the app');
		expect(code).toHaveProperty('autocomplete', 'one-time-code');
		expect(code.getAttribute('inputmode')).toBe('numeric');
	});

	it('ties a rejected password to the field', async () => {
		openAt('/setup#the-token');
		mockAPI({ '/api/setup/begin': [{ status: 422, body: { error: 'Use at least 8 characters' } }] });
		render(<App />);

		const password = screen.getByLabelText('Password');
		await userEvent.type(password, 'short but typed');
		await userEvent.click(screen.getByRole('button', { name: 'Continue' }));

		const error = await screen.findByRole('alert');
		expect(error.textContent).toBe('Use at least 8 characters');
		expect(password.getAttribute('aria-invalid')).toBe('true');
		expect(password.getAttribute('aria-describedby')).toContain(error.id);
	});

	it('explains a missing token', () => {
		openAt('/setup');
		mockAPI({});
		render(<App />);

		expect(screen.getByRole('alert').textContent).toContain('sudo tero reset-login');
	});
});

describe('login page', () => {
	it('points to the setup link before setup', async () => {
		openAt('/');
		mockAPI({ '/api/state': [{ status: 200, body: { isSetUp: false, isLoggedIn: false } }] });
		render(<App />);

		expect((await screen.findByRole('main')).textContent).toContain('sudo tero init');
		expect(screen.queryByLabelText('Password')).toBeNull();
	});

	it('uses login autocomplete and shows one error for the whole form', async () => {
		openAt('/');
		mockAPI({
			'/api/state': [{ status: 200, body: { isSetUp: true, isLoggedIn: false } }],
			'/api/login': [{ status: 401, body: { error: 'Wrong password or code' } }],
		});
		render(<App />);

		const password = await screen.findByLabelText('Password');
		expect(password).toHaveProperty('autocomplete', 'current-password');
		const code = screen.getByLabelText('6-digit code from your authenticator app');
		expect(code).toHaveProperty('autocomplete', 'one-time-code');

		await userEvent.type(password, 'a long enough passphrase');
		await userEvent.type(code, '123456');
		await userEvent.click(screen.getByRole('button', { name: 'Log in' }));

		expect((await screen.findByRole('alert')).textContent).toBe('Wrong password or code');
		expect(code).toHaveProperty('value', '');
	});
});

describe('home page', () => {
	it('lists security events in a table with column headers', async () => {
		openAt('/');
		mockAPI({
			'/api/state': [{ status: 200, body: { isSetUp: true, isLoggedIn: true } }],
			'/api/events': [{ status: 200, body: { events: [{ at: '2026-10-11T10:00:00Z', type: 'login_reset' }] } }],
		});
		render(<App />);

		const table = await screen.findByRole('table');
		expect(screen.getAllByRole('columnheader').map((header) => header.textContent)).toEqual(['Time', 'Event', 'From']);
		expect(table.textContent).toContain('Login reset with sudo tero reset-login');
		expect(table.querySelector('time')?.getAttribute('datetime')).toBe('2026-10-11T10:00:00Z');
	});

	it('goes back to the login form once an idle session has ended (v5.0.0-14.3.1)', async () => {
		vi.useFakeTimers({ shouldAdvanceTime: true });
		openAt('/');
		mockAPI({
			'/api/state': [
				{ status: 200, body: { isSetUp: true, isLoggedIn: true } },
				{ status: 200, body: { isSetUp: true, isLoggedIn: false } },
			],
			'/api/events': [{ status: 200, body: { events: [{ at: '2026-10-11T10:00:00Z', type: 'login_reset' }] } }],
		});
		render(<App />);
		await screen.findByRole('table');

		await vi.advanceTimersByTimeAsync(30 * 60 * 1000 + 5 * 1000);

		expect(await screen.findByRole('heading', { name: 'Log in to Tero' })).toBeTruthy();
		expect(screen.queryByRole('table')).toBeNull();
		vi.useRealTimers();
	});
});
