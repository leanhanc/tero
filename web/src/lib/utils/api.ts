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

/** An error the server explained, safe to show as is. */
export class ApiError extends Error {
	readonly status: number;

	constructor(status: number, message: string) {
		super(message);
		this.status = status;
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

async function request<T>(method: 'GET' | 'POST', path: string, body?: unknown): Promise<T> {
	const hasBody = body !== undefined;
	const response = await fetch(path, {
		method,
		credentials: 'same-origin',
		headers: hasBody ? { 'Content-Type': 'application/json' } : undefined,
		body: hasBody ? JSON.stringify(body) : undefined,
	});

	if (!response.ok) {
		throw new ApiError(response.status, await readErrorMessage(response));
	}

	const hasNoContent = response.status === 204;
	if (hasNoContent) {
		return undefined as T;
	}

	return (await response.json()) as T;
}

async function readErrorMessage(response: Response): Promise<string> {
	try {
		const body = (await response.json()) as { error?: string };
		return body.error ?? 'Something went wrong. Try again.';
	} catch {
		return 'Something went wrong. Try again.';
	}
}

/** The message to show for a failed call, whatever was thrown. */
export function errorMessage(error: unknown): string {
	if (error instanceof ApiError) {
		return error.message;
	}

	return 'The server could not be reached. Check your connection and try again.';
}
