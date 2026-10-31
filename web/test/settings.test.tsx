import { cleanup, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { App } from '../src/app';
import type { Account } from '../src/lib/utils/api';

type MockResponse = { status: number; body?: unknown };

/** Answers API calls in order and records what was sent. */
function mockAPI(responses: Record<string, MockResponse[]>) {
	const calls: { path: string; body: unknown }[] = [];
	vi.stubGlobal(
		'fetch',
		vi.fn(async (path: string, init?: RequestInit) => {
			calls.push({ path, body: init?.body ? JSON.parse(String(init.body)) : undefined });
			const next = responses[path]?.shift() ?? defaultResponses[path] ?? { status: 500, body: { error: 'unexpected call' } };
			return new Response(next.body === undefined ? null : JSON.stringify(next.body), { status: next.status });
		}),
	);
	return calls;
}

const signedIn = { status: 200, body: { isSetUp: true, isLoggedIn: true } };

const notConnected = { status: 200, body: { app: { isConnected: false }, installation: null, repositories: [] } };

// Calls every settings page makes, answered unless a test answers them itself.
const defaultResponses: Record<string, MockResponse> = { '/api/github': notConnected };

const firefox = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 14.5; rv:140.0) Gecko/20100101 Firefox/140.0';

function account(isConfirmed: boolean): Account {
	return {
		isConfirmed,
		sessions: [
			{
				id: 'current-id',
				method: 'password',
				createdAt: '2026-10-24T10:00:00Z',
				lastSeenAt: '2026-10-24T10:05:00Z',
				loginIP: '198.51.100.7',
				lastIP: '198.51.100.7',
				userAgent: firefox,
				isCurrent: true,
			},
			{
				id: 'other-id',
				method: 'passkey',
				createdAt: '2026-10-24T09:00:00Z',
				lastSeenAt: '2026-10-24T09:30:00Z',
				loginIP: '192.0.2.80',
				lastIP: '192.0.2.81',
				userAgent: 'curl/8.5.0',
				isCurrent: false,
			},
		],
		passkeys: [{ id: 'key-id', name: 'Laptop', createdAt: '2026-10-20T10:00:00Z' }],
	};
}

function openSettings() {
	window.history.replaceState(null, '', '/settings');
}

afterEach(() => {
	cleanup();
	vi.unstubAllGlobals();
});

describe('settings page', () => {
	it('asks to confirm when ending a session, then ends it (v5.0.0-7.5.2)', async () => {
		openSettings();
		const calls = mockAPI({
			'/api/state': [signedIn],
			'/api/account': [
				{ status: 200, body: account(false) },
				{ status: 200, body: account(true) },
			],
			'/api/account/confirm': [{ status: 204 }],
			'/api/sessions/end': [{ status: 204 }],
		});
		render(<App />);

		const sessions = await screen.findByRole('table', { name: /Where you are logged in/ });
		expect(within(sessions).getByText('Firefox on macOS (This browser)')).toBeTruthy();
		expect(within(sessions).getByText('curl/8.5.0')).toBeTruthy();
		expect(within(sessions).getByText('192.0.2.81 (logged in from 192.0.2.80)')).toBeTruthy();
		expect(screen.queryByRole('region', { name: "Confirm it's you" })).toBeNull();

		await userEvent.click(screen.getByRole('button', { name: 'End: curl/8.5.0' }));
		expect(calls.some((call) => call.path === '/api/sessions/end')).toBe(false);

		const confirm = screen.getByRole('region', { name: "Confirm it's you" });
		expect(within(confirm).getByLabelText('Password')).toBe(document.activeElement);
		await userEvent.type(within(confirm).getByLabelText('Password'), 'a long enough passphrase');
		await userEvent.type(within(confirm).getByLabelText('6-digit code from your authenticator app'), '123456');
		await userEvent.click(within(confirm).getByRole('button', { name: 'Confirm' }));

		await screen.findByRole('table', { name: /Where you are logged in/ });
		expect(calls.find((call) => call.path === '/api/sessions/end')?.body).toEqual({ id: 'other-id', isAllOthers: false });
		expect(screen.queryByRole('region', { name: "Confirm it's you" })).toBeNull();
	});

	it('changes the password and ends other sessions by default (v5.0.0-6.2.3)', async () => {
		openSettings();
		const calls = mockAPI({
			'/api/state': [signedIn],
			'/api/account': [
				{ status: 200, body: account(false) },
				{ status: 200, body: account(false) },
			],
			'/api/account/password': [
				{ status: 422, body: { error: 'Don\'t use "tero" in the password' } },
				{ status: 204 },
			],
		});
		render(<App />);

		const form = await screen.findByRole('region', { name: 'Password' });
		const newPassword = within(form).getByLabelText('New password');
		expect(newPassword).toHaveProperty('autocomplete', 'new-password');
		expect(within(form).getByLabelText('Current password')).toHaveProperty('autocomplete', 'current-password');
		expect(within(form).getByLabelText('End every other session')).toHaveProperty('checked', true);

		await userEvent.type(within(form).getByLabelText('Current password'), 'the current passphrase');
		await userEvent.type(within(form).getByLabelText('6-digit code from your authenticator app'), '123456');
		await userEvent.type(newPassword, 'my tero passphrase');
		await userEvent.click(within(form).getByRole('button', { name: 'Change password' }));

		const error = await within(form).findByRole('alert');
		expect(newPassword.getAttribute('aria-describedby')).toContain(error.id);
		expect(calls.find((call) => call.path === '/api/account/password')?.body).toEqual({
			currentPassword: 'the current passphrase',
			code: '123456',
			newPassword: 'my tero passphrase',
			isEndingOthers: true,
		});

		await userEvent.type(within(form).getByLabelText('6-digit code from your authenticator app'), '654321');
		await userEvent.clear(newPassword);
		await userEvent.type(newPassword, 'an unrelated passphrase');
		await userEvent.click(within(form).getByRole('button', { name: 'Change password' }));
		expect((await within(form).findByRole('status')).textContent).toBe('Your password was changed.');
	});

	it('lists passkeys with their actions whether or not the session is confirmed', async () => {
		openSettings();
		mockAPI({ '/api/state': [signedIn], '/api/account': [{ status: 200, body: account(false) }] });
		render(<App />);

		const passkeys = await screen.findByRole('table', { name: 'Your passkeys' });
		expect(within(passkeys).getByText('Laptop')).toBeTruthy();
		expect(within(passkeys).getByText('Never')).toBeTruthy();
		expect(screen.getByRole('button', { name: 'Remove Laptop' })).toBeTruthy();
		expect(screen.queryByRole('region', { name: "Confirm it's you" })).toBeNull();
	});

	it('removes a passkey and ends other sessions by default (v5.0.0-7.4.3)', async () => {
		openSettings();
		const calls = mockAPI({
			'/api/state': [signedIn],
			'/api/account': [
				{ status: 200, body: account(true) },
				{ status: 200, body: { ...account(true), passkeys: [] } },
			],
			'/api/passkeys/remove': [{ status: 204 }],
		});
		render(<App />);

		await userEvent.click(await screen.findByRole('button', { name: 'Remove Laptop' }));

		expect(calls.find((call) => call.path === '/api/passkeys/remove')?.body).toEqual({ id: 'key-id', isEndingOthers: true });
		expect((await screen.findByRole('status')).textContent).toBe('Laptop was removed.');
		expect(await screen.findByText('No passkeys yet.')).toBeTruthy();
	});

	it('asks to confirm again when the confirmation ran out, then runs the action', async () => {
		openSettings();
		const calls = mockAPI({
			'/api/state': [signedIn],
			'/api/account': [
				{ status: 200, body: account(true) },
				{ status: 200, body: account(true) },
				{ status: 200, body: account(true) },
			],
			'/api/sessions/end': [
				{ status: 403, body: { error: "Confirm it's you with your password and a code first", code: 'not_confirmed' } },
				{ status: 204 },
			],
			'/api/account/confirm': [{ status: 204 }],
		});
		render(<App />);

		await userEvent.click(await screen.findByRole('button', { name: 'End all other sessions' }));

		const confirm = await screen.findByRole('region', { name: "Confirm it's you" });
		expect(screen.getByRole('button', { name: 'End: curl/8.5.0' })).toHaveProperty('disabled', true);
		await userEvent.type(within(confirm).getByLabelText('Password'), 'a long enough passphrase');
		await userEvent.type(within(confirm).getByLabelText('6-digit code from your authenticator app'), '123456');
		await userEvent.click(within(confirm).getByRole('button', { name: 'Confirm' }));

		await screen.findByRole('button', { name: 'End all other sessions' });
		const ends = calls.filter((call) => call.path === '/api/sessions/end');
		expect(ends).toHaveLength(2);
		expect(ends[1]?.body).toEqual({ id: '', isAllOthers: true });
	});

	it('shows other refusals as errors without asking to confirm', async () => {
		openSettings();
		mockAPI({
			'/api/state': [signedIn],
			'/api/account': [{ status: 200, body: account(true) }],
			'/api/sessions/end': [{ status: 403, body: { error: 'Cross-site requests are not allowed' } }],
		});
		render(<App />);

		await userEvent.click(await screen.findByRole('button', { name: 'End all other sessions' }));

		expect((await screen.findByRole('alert')).textContent).toBe('Cross-site requests are not allowed');
		expect(screen.queryByRole('region', { name: "Confirm it's you" })).toBeNull();
	});

	it('asks for a fresh click to add a passkey after confirming, as Safari needs', async () => {
		openSettings();
		vi.stubGlobal(
			'PublicKeyCredential',
			class {
				static parseCreationOptionsFromJSON = (options: unknown) => options;
			},
		);
		const calls = mockAPI({
			'/api/state': [signedIn],
			'/api/account': [
				{ status: 200, body: account(false) },
				{ status: 200, body: account(true) },
			],
			'/api/account/confirm': [{ status: 204 }],
		});
		render(<App />);

		await userEvent.click(await screen.findByRole('button', { name: 'Add a passkey' }));
		const confirm = screen.getByRole('region', { name: "Confirm it's you" });
		await userEvent.type(within(confirm).getByLabelText('Password'), 'a long enough passphrase');
		await userEvent.type(within(confirm).getByLabelText('6-digit code from your authenticator app'), '123456');
		await userEvent.click(within(confirm).getByRole('button', { name: 'Confirm' }));

		expect((await screen.findByRole('status')).textContent).toBe("You're confirmed. Press Add a passkey to create it.");
		expect(calls.some((call) => call.path === '/api/passkeys/register/begin')).toBe(false);
	});

});

describe('passkey offer after login', () => {
	it('offers a passkey after a password login and remembers "Not now"', async () => {
		window.history.replaceState(null, '', '/');
		localStorage.clear();
		vi.stubGlobal(
			'PublicKeyCredential',
			class {
				static parseCreationOptionsFromJSON = (options: unknown) => options;
			},
		);
		mockAPI({
			'/api/state': [
				{ status: 200, body: { isSetUp: true, isLoggedIn: false } },
				{ status: 200, body: { isSetUp: true, isLoggedIn: true } },
			],
			'/api/login': [{ status: 204 }],
			'/api/account': [{ status: 200, body: { ...account(true), passkeys: [] } }],
			'/api/apps': [{ status: 200, body: { apps: [] } }],
		});
		render(<App />);

		await userEvent.type(await screen.findByLabelText('Password'), 'a long enough passphrase');
		await userEvent.type(screen.getByLabelText('6-digit code from your authenticator app'), '123456');
		await userEvent.click(screen.getByRole('button', { name: 'Log in' }));

		expect(await screen.findByRole('heading', { name: 'Log in faster next time' })).toBeTruthy();
		await userEvent.click(screen.getByRole('button', { name: 'Not now' }));

		expect(await screen.findByRole('heading', { name: 'Apps' })).toBeTruthy();
		expect(localStorage.getItem('tero:passkey-offer-dismissed')).toBe('1');
	});
});

describe('passkey offer is skipped', () => {
	async function logIn(passkeys: Account['passkeys']) {
		window.history.replaceState(null, '', '/');
		vi.stubGlobal(
			'PublicKeyCredential',
			class {
				static parseCreationOptionsFromJSON = (options: unknown) => options;
			},
		);
		mockAPI({
			'/api/state': [
				{ status: 200, body: { isSetUp: true, isLoggedIn: false } },
				{ status: 200, body: { isSetUp: true, isLoggedIn: true } },
			],
			'/api/login': [{ status: 204 }],
			'/api/account': [{ status: 200, body: { ...account(true), passkeys } }],
			'/api/apps': [{ status: 200, body: { apps: [] } }],
		});
		render(<App />);

		await userEvent.type(await screen.findByLabelText('Password'), 'a long enough passphrase');
		await userEvent.type(screen.getByLabelText('6-digit code from your authenticator app'), '123456');
		await userEvent.click(screen.getByRole('button', { name: 'Log in' }));
	}

	it('when the admin already has a passkey', async () => {
		localStorage.clear();
		await logIn(account(true).passkeys);

		expect(await screen.findByRole('heading', { name: 'Apps' })).toBeTruthy();
		expect(screen.queryByRole('heading', { name: 'Log in faster next time' })).toBeNull();
	});

	it('when the admin chose "Not now" in this browser', async () => {
		localStorage.setItem('tero:passkey-offer-dismissed', '1');
		await logIn([]);

		expect(await screen.findByRole('heading', { name: 'Apps' })).toBeTruthy();
		expect(screen.queryByRole('heading', { name: 'Log in faster next time' })).toBeNull();
	});
});

describe('passkey login', () => {
	it('sends the browser passkey answer and logs in (v5.0.0-6.3.3)', async () => {
		window.history.replaceState(null, '', '/');
		const answer = { id: 'cred', type: 'public-key' };
		class FakePublicKeyCredential {
			static parseCreationOptionsFromJSON = (options: unknown) => options;
			static parseRequestOptionsFromJSON = (options: unknown) => ({ parsed: options });
			toJSON() {
				return answer;
			}
		}
		vi.stubGlobal('PublicKeyCredential', FakePublicKeyCredential);
		const get = vi.fn(async () => new FakePublicKeyCredential());
		vi.stubGlobal('navigator', { ...navigator, credentials: { get } });
		const calls = mockAPI({
			'/api/state': [
				{ status: 200, body: { isSetUp: true, isLoggedIn: false } },
				{ status: 200, body: { isSetUp: true, isLoggedIn: true } },
			],
			'/api/login/passkey/begin': [{ status: 200, body: { challengeId: 'challenge', options: { challenge: 'abc' } } }],
			'/api/login/passkey/finish': [{ status: 204 }],
			'/api/apps': [{ status: 200, body: { apps: [] } }],
		});
		render(<App />);

		await userEvent.click(await screen.findByRole('button', { name: 'Log in with a passkey' }));

		expect(await screen.findByRole('heading', { name: 'Apps' })).toBeTruthy();
		expect(get).toHaveBeenCalledWith({ publicKey: { parsed: { challenge: 'abc' } } });
		expect(calls.find((call) => call.path === '/api/login/passkey/finish')?.body).toEqual({
			challengeId: 'challenge',
			credential: answer,
		});
	});

	it('keeps the password form when the browser has no passkey support', async () => {
		window.history.replaceState(null, '', '/');
		mockAPI({ '/api/state': [{ status: 200, body: { isSetUp: true, isLoggedIn: false } }] });
		render(<App />);

		expect(await screen.findByLabelText('Password')).toBeTruthy();
		expect(screen.queryByRole('button', { name: 'Log in with a passkey' })).toBeNull();
	});
});

describe('GitHub section', () => {
	it('confirms first, then sends the manifest to the organization on GitHub', async () => {
		openSettings();
		const calls = mockAPI({
			'/api/state': [signedIn],
			'/api/account': [{ status: 200, body: account(true) }],
			'/api/github/connect/begin': [
				{
					status: 200,
					body: { action: 'https://github.com/organizations/acme/settings/apps/new?state=s', manifest: '{"name":"Tero"}', state: 's' },
				},
			],
		});
		const submitted: { action: string; manifest: string }[] = [];
		vi.spyOn(HTMLFormElement.prototype, 'submit').mockImplementation(function (this: HTMLFormElement) {
			const manifest = this.querySelector<HTMLInputElement>('input[name="manifest"]')?.value ?? '';
			submitted.push({ action: this.action, manifest });
		});
		render(<App />);

		await userEvent.click(await screen.findByLabelText('An organization I own'));
		await userEvent.type(screen.getByLabelText('Organization'), 'acme');
		await userEvent.click(screen.getByRole('button', { name: 'Connect GitHub' }));

		expect(calls.find((call) => call.path === '/api/github/connect/begin')?.body).toEqual({ organization: 'acme' });
		expect(submitted).toEqual([{ action: 'https://github.com/organizations/acme/settings/apps/new?state=s', manifest: '{"name":"Tero"}' }]);
	});

	it('lists the repositories the App can read', async () => {
		openSettings();
		mockAPI({
			'/api/state': [signedIn],
			'/api/account': [{ status: 200, body: account(true) }],
			'/api/github': [
				{
					status: 200,
					body: {
						app: { isConnected: true, slug: 'tero-dash', owner: 'leanhanc' },
						installation: { account: 'leanhanc', isOrganization: false, isSuspended: false },
						repositories: [{ id: 1, owner: 'leanhanc', name: 'api', isPrivate: true, defaultBranch: 'main' }],
						installUrl: 'https://github.com/apps/tero-dash/installations/new',
						settingsUrl: 'https://github.com/settings/apps/tero-dash',
					},
				},
			],
		});
		render(<App />);

		const list = await screen.findByRole('list', { name: 'Repositories the App can read' });
		expect(within(list).getByText('leanhanc/api (private)')).toBeTruthy();
		expect(screen.getByRole('link', { name: 'the App on GitHub' }).getAttribute('href')).toBe('https://github.com/settings/apps/tero-dash');
	});
});
