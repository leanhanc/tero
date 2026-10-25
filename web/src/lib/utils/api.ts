import type { Messages } from '../../locales/en';

// The dashboard's JSON API. Every call is same-origin and sends JSON, which
// the server requires to block cross-site requests.

export type DashboardState = {
	isSetUp: boolean;
	isLoggedIn: boolean;
};

export type Enrolment = {
	secret: string;
	uri: string;
	qrCode: string;
};

export type DeployState = 'queued' | 'building' | 'starting' | 'live' | 'failed' | 'superseded';

export type AppSummary = {
	/** The first label of the app's address. It never changes. */
	name: string;
	displayName: string;
	address: string;
	latestState: DeployState | '';
};

export type Deploy = {
	id: number;
	state: DeployState;
	failedStep: string;
	reason: string;
	commitSha: string;
	commitMessage: string;
	queuedAt: number;
	finishedAt: number;
};

export type AppDetail = {
	name: string;
	displayName: string;
	address: string;
	repository: string;
	branch: string;
	rootDirectory: string;
	runTogether: boolean;
	deploys: Deploy[];
};

export type RepositoryLookup = {
	repository: string;
	defaultBranch: string;
	repositoryName: string;
	/** What every app's address ends in, such as 203.0.113.10.nip.io. */
	appDomain: string;
};

export type NewApp = {
	repository: string;
	branch: string;
	displayName: string;
	name: string;
	rootDirectory: string;
};

export type LoginMethod = 'password' | 'passkey' | 'setup';

export type SessionInfo = {
	/** A random id for ending the session; never its token. */
	id: string;
	method: LoginMethod;
	createdAt: string;
	lastSeenAt: string;
	loginIP: string;
	lastIP: string;
	userAgent: string;
	isCurrent: boolean;
};

export type PasskeyInfo = {
	id: string;
	name: string;
	createdAt: string;
	/** Missing until the passkey logs in for the first time. */
	lastUsedAt?: string;
};

export type Account = {
	/** Whether this session confirmed it's the admin in the last few minutes. */
	isConfirmed: boolean;
	sessions: SessionInfo[];
	passkeys: PasskeyInfo[];
};

/** WebAuthn options from the key service, and the id that finishes the ceremony. */
export type PasskeyOptions = {
	challengeId: string;
	options: unknown;
};

export type PasswordChange = {
	currentPassword: string;
	code: string;
	newPassword: string;
	isEndingOthers: boolean;
};

/** An error the server explained, safe to show as is. */
export class ApiError extends Error {
	readonly status: number;
	/** The form field the error is about, when the server names one. */
	readonly field: string;

	constructor(status: number, message: string, field = '') {
		super(message);
		this.status = status;
		this.field = field;
	}
}

export function getState(): Promise<DashboardState> {
	return request<DashboardState>('GET', '/api/state');
}

export function beginSetup(token: string, password: string): Promise<Enrolment> {
	return request<Enrolment>('POST', '/api/setup/begin', { token, password });
}

export function finishSetup(token: string, code: string): Promise<void> {
	return request<void>('POST', '/api/setup/finish', { token, code });
}

export function login(password: string, code: string): Promise<void> {
	return request<void>('POST', '/api/login', { password, code });
}

export function beginPasskeyLogin(): Promise<PasskeyOptions> {
	return request<PasskeyOptions>('POST', '/api/login/passkey/begin', {});
}

export function finishPasskeyLogin(challengeId: string, credential: unknown): Promise<void> {
	return request<void>('POST', '/api/login/passkey/finish', { challengeId, credential });
}

export function logout(): Promise<void> {
	return request<void>('POST', '/api/logout', {});
}

export function lookUpRepository(url: string): Promise<RepositoryLookup> {
	return request<RepositoryLookup>('POST', '/api/repositories/lookup', { url });
}

export async function createApp(newApp: NewApp): Promise<string> {
	const response = await request<{ name: string }>('POST', '/api/apps', newApp);
	return response.name;
}

export async function listApps(): Promise<AppSummary[]> {
	const response = await request<{ apps: AppSummary[] }>('GET', '/api/apps');
	return response.apps;
}

export function getApp(name: string): Promise<AppDetail> {
	return request<AppDetail>('GET', `/api/apps/${encodeURIComponent(name)}`);
}

export function redeploy(name: string): Promise<void> {
	return request<void>('POST', `/api/apps/${encodeURIComponent(name)}/deploys`, {});
}

export function setDisplayName(name: string, displayName: string): Promise<void> {
	return request<void>('POST', `/api/apps/${encodeURIComponent(name)}/display-name`, { displayName });
}

export function setRunTogether(name: string, runTogether: boolean): Promise<void> {
	return request<void>('POST', `/api/apps/${encodeURIComponent(name)}/settings`, { runTogether });
}

export function getAccount(): Promise<Account> {
	return request<Account>('GET', '/api/account');
}

export function confirmIdentity(password: string, code: string): Promise<void> {
	return request<void>('POST', '/api/account/confirm', { password, code });
}

export function changePassword(change: PasswordChange): Promise<void> {
	return request<void>('POST', '/api/account/password', change);
}

export function endSession(id: string): Promise<void> {
	return request<void>('POST', '/api/sessions/end', { id, isAllOthers: false });
}

export function endOtherSessions(): Promise<void> {
	return request<void>('POST', '/api/sessions/end', { id: '', isAllOthers: true });
}

export function beginPasskeyRegistration(): Promise<PasskeyOptions> {
	return request<PasskeyOptions>('POST', '/api/passkeys/register/begin', {});
}

export function finishPasskeyRegistration(
	challengeId: string,
	name: string,
	credential: unknown,
	isEndingOthers: boolean,
): Promise<PasskeyInfo> {
	return request<PasskeyInfo>('POST', '/api/passkeys/register/finish', { challengeId, name, credential, isEndingOthers });
}

export function renamePasskey(id: string, name: string): Promise<void> {
	return request<void>('POST', '/api/passkeys/rename', { id, name });
}

export function removePasskey(id: string, isEndingOthers: boolean): Promise<void> {
	return request<void>('POST', '/api/passkeys/remove', { id, isEndingOthers });
}

/** The address of a deploy's build log, streamed as Server-Sent Events. */
export function deployLogURL(deployID: number): string {
	return `/api/deploys/${deployID}/log`;
}

async function request<T>(method: 'GET' | 'POST', path: string, body?: unknown): Promise<T> {
	const hasBody = body !== undefined;
	const response = await fetch(path, {
		method,
		credentials: 'same-origin',
		headers: hasBody ? { 'Content-Type': 'application/json' } : undefined,
		body: hasBody ? JSON.stringify(body) : undefined,
	});

	if (!response.ok) {
		const { message, field } = await readError(response);
		throw new ApiError(response.status, message, field);
	}

	const hasNoContent = response.status === 204;
	if (hasNoContent) {
		return undefined as T;
	}

	return (await response.json()) as T;
}

// An empty message means the server gave no explanation; errorMessage turns
// it into a generic one in the dashboard's language.
async function readError(response: Response): Promise<{ message: string; field: string }> {
	try {
		const body = (await response.json()) as { error?: string; field?: string };
		return { message: body.error ?? '', field: body.field ?? '' };
	} catch {
		return { message: '', field: '' };
	}
}

/** The message to show for a failed call, whatever was thrown. */
export function errorMessage(error: unknown, messages: Messages['errors']): string {
	if (!(error instanceof ApiError)) {
		return messages.unreachable;
	}

	return error.message || messages.generic;
}
