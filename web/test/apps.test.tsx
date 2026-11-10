import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { App } from '../src/app';

type MockResponse = { status: number; body?: unknown };

const loggedIn = { status: 200, body: { isSetUp: true, isLoggedIn: true } };
const noSource = { repository: '', branch: '', rootDirectory: '' };
const appSource = { repository: 'https://github.com/leanhanc/api', branch: 'main', rootDirectory: '' };
const noVariables = { status: 200, body: { variables: [], approvedSource: noSource, source: appSource, isSourceApproved: true, isRepositoryReplaced: false } };

/** Answers API calls in order and records what was sent. */
function mockAPI(responses: Record<string, MockResponse[]>) {
	const calls: { path: string; body: unknown }[] = [];
	vi.stubGlobal(
		'fetch',
		vi.fn(async (path: string, init?: RequestInit) => {
			calls.push({ path, body: init?.body ? JSON.parse(String(init.body)) : undefined });
			const queue = responses[path];
			const fallback = path.endsWith('/variables') ? noVariables : { status: 500, body: { error: 'unexpected call' } };
			const next = (queue && queue.length > 1 ? queue.shift() : queue?.[0]) ?? fallback;
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
	deployOnPush: true,
	pollError: '',
	startProblem: '',
	pendingChanges: [] as string[],
	pendingNeedsBuild: false,
	deploys: [
		{
			id: 7,
			state: 'failed',
			failedStep: 'building',
			reason: buildFailedReason,
			commitSha: 'abcdef1234567890',
			commitMessage: 'feat: break the build',
			trigger: 'push',
			pushedBy: 'leanhanc',
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

	it('queues a redeploy behind a running deploy, then shows it queued', async () => {
		openAt('/apps/api');
		const runningDeploy = { ...failedApp.deploys[0], state: 'building', reason: '', failedStep: '' };
		const building = { ...failedApp, deploys: [runningDeploy] };
		const queued = { ...failedApp, deploys: [{ ...runningDeploy, id: 8, state: 'queued', trigger: 'manual', pushedBy: '' }, runningDeploy] };
		const calls = mockAPI({
			'/api/state': [loggedIn],
			'/api/apps/api': [
				{ status: 200, body: building },
				{ status: 200, body: queued },
			],
			'/api/apps/api/deploys': [{ status: 202 }],
		});
		const opened = mockEventSource([]);
		render(<App />);

		expect(await screen.findByText('A deploy is running. Redeploying queues another one after it.')).toBeTruthy();
		await userEvent.click(screen.getByRole('button', { name: 'Redeploy' }));

		const queuedButton = await screen.findByRole('button', { name: 'Queued' });
		expect(queuedButton).toHaveProperty('disabled', true);
		expect(calls.some((call) => call.path === '/api/apps/api/deploys')).toBe(true);
		expect(opened.every((path) => path === '/api/deploys/7/log')).toBe(true);
	});

	it('says who started each deploy', async () => {
		openAt('/apps/api');
		mockAPI({ '/api/state': [loggedIn], '/api/apps/api': [{ status: 200, body: failedApp }] });
		mockEventSource([]);
		render(<App />);

		expect(await screen.findByText('A push by leanhanc')).toBeTruthy();
	});

	it('turns off deploy on push and keeps the other setting', async () => {
		openAt('/apps/api');
		const calls = mockAPI({
			'/api/state': [loggedIn],
			'/api/apps/api': [
				{ status: 200, body: failedApp },
				{ status: 200, body: { ...failedApp, deployOnPush: false } },
			],
			'/api/apps/api/settings': [{ status: 204 }],
		});
		mockEventSource([]);
		render(<App />);

		const checkbox = await screen.findByLabelText<HTMLInputElement>('Deploy every new commit on the branch');
		expect(checkbox.checked).toBe(true);
		await userEvent.click(checkbox);

		expect(calls.find((call) => call.path === '/api/apps/api/settings')?.body).toEqual({ deployOnPush: false });
	});

	it('shows how long a deploy has run and says so when its log goes quiet', async () => {
		vi.useFakeTimers({ shouldAdvanceTime: true });
		vi.setSystemTime(new Date('2026-10-21T22:00:00Z'));
		openAt('/apps/api');
		const queuedAt = Date.parse('2026-10-21T21:56:48Z') / 1000;
		const building = {
			...failedApp,
			deploys: failedApp.deploys.map((deploy) => ({ ...deploy, state: 'building', reason: '', failedStep: '', queuedAt })),
		};
		mockAPI({ '/api/state': [loggedIn], '/api/apps/api': [{ status: 200, body: building }] });
		mockEventSource([{ type: 'log', data: JSON.stringify('#15 npm install\n') }]);
		render(<App />);

		expect(await screen.findByText('Running for 3m 12s.')).toBeTruthy();
		expect(screen.queryByText(/No new output/)).toBeNull();

		await vi.advanceTimersByTimeAsync(31_000);

		expect(screen.getByText('Running for 3m 43s.')).toBeTruthy();
		expect(screen.getByText(/^No new output for 31s\. The deploy is still running/)).toBeTruthy();
		vi.useRealTimers();
	});
});

describe('new app from GitHub', () => {
	it('creates an app from a repository the App can read', async () => {
		openAt('/apps/new');
		const calls = mockAPI({
			'/api/state': [loggedIn],
			'/api/github': [
				{
					status: 200,
					body: {
						app: { isConnected: true, slug: 'tero-dash' },
						installation: { account: 'leanhanc', isOrganization: false, isSuspended: false },
						repositories: [{ id: 555, owner: 'leanhanc', name: 'secret-api', isPrivate: true, defaultBranch: 'trunk' }],
					},
				},
			],
			'/api/apps': [{ status: 201, body: { name: 'secret-api' } }],
			'/api/apps/secret-api': [{ status: 200, body: { ...failedApp, name: 'secret-api' } }],
		});
		mockEventSource([]);
		render(<App />);

		await userEvent.selectOptions(await screen.findByLabelText('Or pick one the GitHub App can read'), '555');
		expect(screen.getByLabelText<HTMLInputElement>('Branch').value).toBe('trunk');
		await userEvent.click(screen.getByRole('button', { name: 'Create and deploy' }));

		const created = calls.find((call) => call.path === '/api/apps')?.body as Record<string, unknown>;
		expect(created.githubRepositoryId).toBe(555);
		expect(created.repository).toBe('https://github.com/leanhanc/secret-api');
	});
});

const sealedVariable = { name: 'API_KEY', scope: 'runtime', isSealed: true, replacedAt: 1_800_000_000 };
const plainVariable = { name: 'FOO', value: 'bar', scope: 'both', isSealed: false, replacedAt: 1_800_000_000 };
const withVariables = (...variables: unknown[]) => ({
	status: 200,
	body: { variables, approvedSource: appSource, source: appSource, isSourceApproved: true, isRepositoryReplaced: false },
});

function savedChanges(calls: { path: string; body: unknown }[]) {
	return (calls.find((call) => call.path === '/api/apps/api/variables' && call.body)?.body as { changes: unknown[] }).changes;
}

describe('variables', () => {
	// app-variables: New variable
	it('saves a new variable as both, and deploys nothing', async () => {
		openAt('/apps/api');
		const calls = mockAPI({
			'/api/state': [loggedIn],
			'/api/apps/api': [{ status: 200, body: failedApp }],
			'/api/apps/api/variables': [noVariables, { status: 204 }, withVariables(plainVariable)],
		});
		mockEventSource([]);
		render(<App />);

		await userEvent.click(await screen.findByRole('button', { name: 'Add a variable' }));
		await userEvent.type(screen.getByLabelText('Name of the new variable'), 'FOO');
		await userEvent.type(screen.getByLabelText('Value of FOO'), 'bar');
		await userEvent.click(screen.getByRole('button', { name: 'Save variables' }));

		await waitFor(() => expect(savedChanges(calls)).toEqual([{ name: 'FOO', value: 'bar', scope: 'both', isSealed: false }]));
		expect(calls.some((call) => call.path.endsWith('/deploys') || call.path.endsWith('/redeploy'))).toBe(false);
	});

	// app-variables: Sealing switches the scope
	it('switches a variable to runtime only when it is sealed', async () => {
		openAt('/apps/api');
		mockAPI({
			'/api/state': [loggedIn],
			'/api/apps/api': [{ status: 200, body: failedApp }],
			'/api/apps/api/variables': [withVariables(plainVariable)],
		});
		mockEventSource([]);
		render(<App />);

		const scope = await screen.findByLabelText<HTMLSelectElement>('Where FOO is used');
		expect(scope.value).toBe('both');
		await userEvent.click(screen.getByLabelText('Seal FOO'));

		expect(scope.value).toBe('runtime');
		expect(screen.getByText(/Sealing can't be undone/)).toBeTruthy();
		await userEvent.selectOptions(scope, 'both');
		expect(scope.value).toBe('both');
	});

	// app-variables: Sealed value never returned, No way back
	it('offers only replacing and removing a sealed variable, and keeps its value without a new one', async () => {
		openAt('/apps/api');
		const calls = mockAPI({
			'/api/state': [loggedIn],
			'/api/apps/api': [{ status: 200, body: failedApp }],
			'/api/apps/api/variables': [withVariables(sealedVariable), { status: 204 }, withVariables(sealedVariable)],
		});
		mockEventSource([]);
		render(<App />);

		const value = await screen.findByLabelText<HTMLTextAreaElement>('Value of API_KEY');
		expect(value.value).toBe('');
		expect(screen.queryByLabelText('Seal API_KEY')).toBeNull();
		expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Save variables' }).disabled).toBe(true);

		await userEvent.selectOptions(screen.getByLabelText('Where API_KEY is used'), 'both');
		await userEvent.click(screen.getByRole('button', { name: 'Save variables' }));

		await waitFor(() => expect(savedChanges(calls)).toEqual([{ name: 'API_KEY', scope: 'both', isSealed: true }]));
	});

	// app-variables: Unconfirmed seal, Several saves after one check
	it('asks for a fresh check when the server wants one, then saves', async () => {
		openAt('/apps/api');
		const calls = mockAPI({
			'/api/state': [loggedIn],
			'/api/apps/api': [{ status: 200, body: failedApp }],
			'/api/apps/api/variables': [
				withVariables(sealedVariable),
				{ status: 403, body: { error: 'Confirm', code: 'not_confirmed' } },
				{ status: 204 },
				withVariables(sealedVariable),
			],
			'/api/account/confirm': [{ status: 204 }],
		});
		mockEventSource([]);
		render(<App />);

		await userEvent.type(await screen.findByLabelText('Value of API_KEY'), 'new');
		await userEvent.click(screen.getByRole('button', { name: 'Save variables' }));

		await userEvent.type(await screen.findByLabelText('Password'), 'pw');
		await userEvent.type(screen.getByLabelText('6-digit code from your authenticator app'), '123456');
		await userEvent.click(screen.getByRole('button', { name: 'Confirm' }));

		await waitFor(() => expect(calls.filter((call) => call.path === '/api/apps/api/variables' && call.body).length).toBe(2));
	});

	// app-variables: Paste then save, Paste isn't saved by itself, Paste names a sealed variable
	it('fills the form from a pasted .env file and skips sealed names', async () => {
		openAt('/apps/api');
		const calls = mockAPI({
			'/api/state': [loggedIn],
			'/api/apps/api': [{ status: 200, body: failedApp }],
			'/api/apps/api/variables': [withVariables(sealedVariable, plainVariable), { status: 204 }, withVariables(sealedVariable)],
		});
		mockEventSource([]);
		render(<App />);

		const paste = await screen.findByLabelText('Contents of the .env file');
		await userEvent.click(paste);
		await userEvent.paste('export A="1"\n# comment\nFOO=changed\nAPI_KEY=nope\nbad line');
		await userEvent.click(screen.getByRole('button', { name: 'Add to the form' }));

		expect(screen.getByText(/can only be replaced in their own row: API_KEY/)).toBeTruthy();
		expect(screen.getByText(/couldn't be read: 5/)).toBeTruthy();
		expect(calls.some((call) => call.path === '/api/apps/api/variables' && call.body)).toBe(false);

		await userEvent.click(screen.getByRole('button', { name: 'Save variables' }));
		await waitFor(() =>
			expect(savedChanges(calls)).toEqual([
				{ name: 'FOO', value: 'changed', scope: 'both', isSealed: false },
				{ name: 'A', value: '1', scope: 'both', isSealed: false },
			]),
		);
	});

	// app-variables: Save deploys nothing, Runtime variable changed
	it('says how many variables changed and redeploys them', async () => {
		openAt('/apps/api');
		const calls = mockAPI({
			'/api/state': [loggedIn],
			'/api/apps/api': [{ status: 200, body: { ...failedApp, pendingChanges: ['A', 'B'], pendingNeedsBuild: false } }],
			'/api/apps/api/variables/redeploy': [{ status: 202 }],
		});
		mockEventSource([]);
		render(<App />);

		expect(await screen.findByText('2 variables changed. Redeploy to apply them.')).toBeTruthy();
		expect(screen.getByText('Restarts the app with the saved values, with no build.')).toBeTruthy();

		const section = screen.getByRole('region', { name: 'Changes waiting for a redeploy' });
		await userEvent.click(within(section).getByRole('button', { name: 'Redeploy' }));
		await waitFor(() => expect(calls.some((call) => call.path === '/api/apps/api/variables/redeploy')).toBe(true));
	});

	// app-variables: Approving a new source
	it('shows both sources and approves the current one', async () => {
		openAt('/apps/api');
		const calls = mockAPI({
			'/api/state': [loggedIn],
			'/api/apps/api': [{ status: 200, body: failedApp }],
			'/api/apps/api/variables': [
				{
					status: 200,
					body: {
						variables: [plainVariable],
						approvedSource: { ...appSource, repository: 'https://github.com/leanhanc/old' },
						source: appSource,
						isSourceApproved: false,
						isRepositoryReplaced: false,
					},
				},
				withVariables(plainVariable),
			],
			'/api/apps/api/variables/approve-source': [{ status: 204 }],
		});
		mockEventSource([]);
		render(<App />);

		expect(await screen.findByText('https://github.com/leanhanc/old, branch main')).toBeTruthy();
		await userEvent.click(screen.getByRole('button', { name: 'Approve the current source' }));
		await waitFor(() => expect(calls.some((call) => call.path === '/api/apps/api/variables/approve-source')).toBe(true));
	});

	// Audit finding TERO-06: a repository took over the approved address.
	it('explains a repository that replaced the approved one', async () => {
		openAt('/apps/api');
		mockAPI({
			'/api/state': [loggedIn],
			'/api/apps/api': [{ status: 200, body: failedApp }],
			'/api/apps/api/variables': [
				{
					status: 200,
					body: {
						variables: [plainVariable],
						approvedSource: appSource,
						source: appSource,
						isSourceApproved: false,
						isRepositoryReplaced: true,
					},
				},
			],
		});
		mockEventSource([]);
		render(<App />);

		expect(await screen.findByText(/GitHub now has a different repository at this address/)).toBeTruthy();
		expect(screen.getByRole('button', { name: 'Approve the current source' })).toBeTruthy();
	});

	// app-variables: Release shown
	it('lists when a sealed variable was read', async () => {
		openAt('/apps/api');
		mockAPI({
			'/api/state': [loggedIn],
			'/api/apps/api': [{ status: 200, body: failedApp }],
			'/api/apps/api/variables': [withVariables(sealedVariable)],
			'/api/apps/api/variables/API_KEY/reads': [{ status: 200, body: [{ at: 1_800_000_000, phase: 'run', deployId: 7 }] }],
		});
		mockEventSource([]);
		render(<App />);

		await userEvent.click(await screen.findByRole('button', { name: 'Show when it was read' }));
		expect(await screen.findByText(/to run deploy 7/)).toBeTruthy();
	});
});
