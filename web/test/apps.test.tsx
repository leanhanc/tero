import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { App } from '../src/app';

type MockResponse = { status: number; body?: unknown };

const loggedIn = { status: 200, body: { isSetUp: true, isLoggedIn: true } };

/** Answers API calls in order and records what was sent. */
function mockAPI(responses: Record<string, MockResponse[]>) {
	const calls: { path: string; body: unknown }[] = [];
	vi.stubGlobal(
		'fetch',
		vi.fn(async (path: string, init?: RequestInit) => {
			calls.push({ path, body: init?.body ? JSON.parse(String(init.body)) : undefined });
			const queue = responses[path];
			const next = (queue && queue.length > 1 ? queue.shift() : queue?.[0]) ?? { status: 500, body: { error: 'unexpected call' } };
			return new Response(next.body === undefined ? null : JSON.stringify(next.body), { status: next.status });
		}),
	);
	return calls;
}

/** A stand-in EventSource that replays the given events once listeners are attached. */
function mockEventSource(events: { type: string; data: string }[]) {
	const opened: string[] = [];
	class FakeEventSource {
		private listeners = new Map<string, (event: MessageEvent<string>) => void>();

		constructor(url: string) {
			opened.push(url);
			queueMicrotask(() => {
				for (const event of events) {
					this.listeners.get(event.type)?.(new MessageEvent(event.type, { data: event.data }));
				}
			});
		}

		addEventListener(type: string, listener: (event: MessageEvent<string>) => void) {
			this.listeners.set(type, listener);
		}

		close() {}
	}
	vi.stubGlobal('EventSource', FakeEventSource);
	return opened;
}

function openAt(url: string) {
	window.history.replaceState(null, '', url);
}

afterEach(() => {
	cleanup();
	vi.unstubAllGlobals();
});

const buildFailedReason = 'A build step failed. The build log above shows which one and why';

const failedApp = {
	name: 'api',
	displayName: 'Shop API',
	address: 'https://api.203.0.113.10.nip.io',
	repository: 'https://github.com/leanhanc/api',
	branch: 'main',
	rootDirectory: '',
	runTogether: true,
	deploys: [
		{
			id: 7,
			state: 'failed',
			failedStep: 'building',
			reason: buildFailedReason,
			commitSha: 'abcdef1234567890',
			commitMessage: 'feat: break the build',
			queuedAt: 1_800_000_000,
			finishedAt: 1_800_000_060,
		},
	],
};

describe('apps', () => {
	it('lists apps with their address and latest deploy state', async () => {
		openAt('/');
		mockAPI({
			'/api/state': [loggedIn],
			'/api/apps': [{ status: 200, body: { apps: [{ name: 'api', displayName: 'Shop API', address: 'https://api.203.0.113.10.nip.io', latestState: 'live' }] } }],
			'/api/events': [{ status: 200, body: { events: [] } }],
		});
		render(<App />);

		expect((await screen.findByRole('link', { name: 'Shop API' })).getAttribute('href')).toBe('/apps/api');
		expect(screen.getByRole('link', { name: 'https://api.203.0.113.10.nip.io' })).toBeTruthy();
		expect(screen.getByText('Live')).toBeTruthy();

		const help = screen.getByRole('button', { name: 'What is nip.io?' });
		expect(help.getAttribute('aria-expanded')).toBe('false');
		await userEvent.click(help);
		expect(help.getAttribute('aria-expanded')).toBe('true');
		expect(screen.getByText(/no traffic goes through nip\.io/)).toBeTruthy();
	});

	it('leaves out the nip.io help for local addresses', async () => {
		openAt('/');
		mockAPI({
			'/api/state': [loggedIn],
			'/api/apps': [{ status: 200, body: { apps: [{ name: 'api', displayName: 'Shop API', address: 'https://api.localhost', latestState: 'live' }] } }],
			'/api/events': [{ status: 200, body: { events: [] } }],
		});
		render(<App />);

		await screen.findByRole('link', { name: 'Shop API' });
		expect(screen.queryByRole('button', { name: 'What is nip.io?' })).toBeNull();
	});

	it('fills branch, name and address from the repository and ties errors to their field', async () => {
		openAt('/apps/new');
		const calls = mockAPI({
			'/api/state': [loggedIn],
			'/api/repositories/lookup': [
				{
					status: 200,
					body: {
						repository: 'https://github.com/leanhanc/tero',
						defaultBranch: 'main',
						repositoryName: 'Tero',
						appDomain: '203.0.113.10.nip.io',
					},
				},
			],
			'/api/apps': [{ status: 422, body: { error: 'Another app already uses this address', field: 'name' } }],
		});
		render(<App />);

		await userEvent.type(await screen.findByLabelText('GitHub repository'), 'https://github.com/leanhanc/tero.git');
		await userEvent.tab();

		await waitFor(() => expect(screen.getByLabelText<HTMLInputElement>('Branch').value).toBe('main'));
		expect(screen.getByLabelText<HTMLInputElement>('Name').value).toBe('Tero');
		expect(screen.getByLabelText<HTMLInputElement>('Address').value).toBe('tero');
		expect(screen.getByText(/It will be https:\/\/tero\.203\.0\.113\.10\.nip\.io\./)).toBeTruthy();

		await userEvent.click(screen.getByRole('button', { name: 'Create and deploy' }));

		const address = screen.getByLabelText('Address');
		const error = await screen.findByRole('alert');
		expect(error.textContent).toBe('Another app already uses this address');
		expect(address.getAttribute('aria-invalid')).toBe('true');
		expect(address.getAttribute('aria-describedby')).toContain(error.id);
		expect(calls.find((call) => call.path === '/api/apps')?.body).toEqual({
			repository: 'https://github.com/leanhanc/tero',
			branch: 'main',
			displayName: 'Tero',
			name: 'tero',
			rootDirectory: '',
		});
	});

	it('derives the address from the name until the address is edited', async () => {
		openAt('/apps/new');
		mockAPI({ '/api/state': [loggedIn] });
		render(<App />);

		const displayName = await screen.findByLabelText<HTMLInputElement>('Name');
		const address = screen.getByLabelText<HTMLInputElement>('Address');

		await userEvent.type(displayName, 'Tienda Café 2');
		expect(address.value).toBe('tienda-cafe-2');

		await userEvent.clear(address);
		await userEvent.type(address, 'shop');
		await userEvent.type(displayName, '!');
		expect(address.value).toBe('shop');

		await userEvent.clear(address);
		await userEvent.type(displayName, ' Norte');
		expect(address.value).toBe('tienda-cafe-2-norte');
	});

	it('renames an app without changing its address', async () => {
		openAt('/apps/api');
		const renamedApp = { ...failedApp, displayName: 'Storefront API' };
		const calls = mockAPI({
			'/api/state': [loggedIn],
			'/api/apps/api': [
				{ status: 200, body: failedApp },
				{ status: 200, body: renamedApp },
			],
			'/api/apps/api/display-name': [{ status: 204 }],
		});
		mockEventSource([]);
		render(<App />);

		const name = await screen.findByLabelText<HTMLInputElement>('Name');
		await userEvent.clear(name);
		await userEvent.type(name, 'Storefront API');
		await userEvent.click(screen.getByRole('button', { name: 'Save name' }));

		expect(await screen.findByRole('heading', { name: 'Storefront API' })).toBeTruthy();
		expect(calls.find((call) => call.path === '/api/apps/api/display-name')?.body).toEqual({ displayName: 'Storefront API' });
	});

	it('shows why a deploy failed and its log as plain text', async () => {
		openAt('/apps/api');
		mockAPI({ '/api/state': [loggedIn], '/api/apps/api': [{ status: 200, body: failedApp }] });
		const opened = mockEventSource([
			{ type: 'log', data: JSON.stringify('#3 ERROR: <img src=x onerror=alert(1)>\n') },
			{ type: 'done', data: JSON.stringify('failed') },
		]);
		render(<App />);

		expect(await screen.findByText('Failed while building')).toBeTruthy();
		expect(screen.getByText(buildFailedReason)).toBeTruthy();
		expect(opened).toEqual(['/api/deploys/7/log']);

		const log = await screen.findByText(/#3 ERROR/);
		expect(log.tagName).toBe('PRE');
		expect(log.querySelector('img')).toBeNull();
	});

	it('disables Redeploy while a deploy is in progress', async () => {
		openAt('/apps/api');
		const building = { ...failedApp, deploys: failedApp.deploys.map((deploy) => ({ ...deploy, state: 'building', reason: '', failedStep: '' })) };
		mockAPI({ '/api/state': [loggedIn], '/api/apps/api': [{ status: 200, body: building }] });
		mockEventSource([]);
		render(<App />);

		const button = await screen.findByRole('button', { name: 'Redeploy' });
		expect(button).toHaveProperty('disabled', true);
		expect(screen.getByText('A deploy is in progress.')).toBeTruthy();
	});
});
