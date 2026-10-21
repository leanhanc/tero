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

export type SecurityEvent = {
	at: string;
	type: string;
	ip?: string;
	detail?: string;
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

export function logout(): Promise<void> {
	return request<void>('POST', '/api/logout', {});
}

export async function getEvents(): Promise<SecurityEvent[]> {
	const response = await request<{ events: SecurityEvent[] }>('GET', '/api/events');
	return response.events;
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

async function readError(response: Response): Promise<{ message: string; field: string }> {
	const fallback = 'Something went wrong. Try again.';
	try {
		const body = (await response.json()) as { error?: string; field?: string };
		return { message: body.error ?? fallback, field: body.field ?? '' };
	} catch {
		return { message: fallback, field: '' };
	}
}

/** The message to show for a failed call, whatever was thrown. */
export function errorMessage(error: unknown): string {
	if (error instanceof ApiError) {
		return error.message;
	}

	return 'The server could not be reached. Check your connection and try again.';
}
