import { type GitHubRequest, type GitHubStart, getGitHubRequest } from './api';

// How often a page asks whether the agent answered. It checks every second.
const requestPollMs = 500;
// The agent answers within seconds; past this the page says so.
const requestTimeoutMs = 60_000;

/**
 * Sends the browser to GitHub to create the App, by posting its manifest the
 * way GitHub's manifest flow asks: a form, not a fetch.
 */
export function postManifest(start: GitHubStart): void {
	const form = document.createElement('form');
	form.method = 'post';
	form.action = start.action;

	const manifest = document.createElement('input');
	manifest.type = 'hidden';
	manifest.name = 'manifest';
	manifest.value = start.manifest;
	form.append(manifest);

	document.body.append(form);
	form.submit();
}

/** Waits for the agent to answer a request, or gives up after a minute. */
export async function waitForGitHubRequest(requestId: number): Promise<GitHubRequest> {
	const deadline = Date.now() + requestTimeoutMs;
	while (Date.now() < deadline) {
		const answer = await getGitHubRequest(requestId);
		if (answer.status !== 'pending') {
			return answer;
		}
		await new Promise((resolve) => window.setTimeout(resolve, requestPollMs));
	}

	return { status: 'failed', result: '' };
}
