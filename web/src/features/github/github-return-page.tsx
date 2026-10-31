import { useEffect, useRef, useState } from 'react';

import { InternalLink } from '../../components/internal-link';
import { PageHeader } from '../../components/page-header';
import { errorMessage, finishGitHubConnection, getGitHub, refreshGitHub } from '../../lib/utils/api';
import { waitForGitHubRequest } from '../../lib/utils/github';
import { useMessages } from '../../lib/utils/locale';
import { navigate } from '../../lib/utils/navigation';

/** Why returning from GitHub failed, when it did. An empty reason gets the generic message. */
type Outcome = { isFailed: false } | { isFailed: true; reason: string };

/**
 * Where GitHub sends the admin back to. After the App is created
 * (/github/connected), the page asks the agent to store it and sends the
 * admin on to install it. After it is installed (/github/installed), the
 * page asks the agent to read the installation and opens Settings.
 */
export function GitHubReturnPage({ path, onLogout }: { path: string; onLogout: () => void }) {
	const messages = useMessages();
	const [outcome, setOutcome] = useState<Outcome>({ isFailed: false });
	const isConnecting = path === '/github/connected';
	const workingMessage = isConnecting ? messages.github.savingApp : messages.github.readingInstallation;
	// GitHub's code and the state work once, so the page finishes once.
	const hasStarted = useRef(false);

	useEffect(() => {
		if (hasStarted.current) {
			return;
		}
		hasStarted.current = true;

		const finish = isConnecting ? finishConnecting : finishInstalling;
		const query = new URLSearchParams(window.location.search);
		// They are spent once read, so they don't stay in the address bar or
		// the history either.
		window.history.replaceState(null, '', path);
		finish(query)
			.then(setOutcome)
			.catch((failure: unknown) => setOutcome({ isFailed: true, reason: errorMessage(failure, messages.errors) }));
	}, [path, isConnecting, messages]);

	return (
		<>
			<PageHeader onLogout={onLogout} />
			<main>
				<h1>{messages.github.heading}</h1>
				{outcome.isFailed ? (
					<>
						<p role="alert">{outcome.reason || messages.github.failed}</p>
						<InternalLink to="/settings">{messages.github.backToSettings}</InternalLink>
					</>
				) : (
					<p role="status">{workingMessage}</p>
				)}
			</main>
		</>
	);
}

/** Stores the new App through the agent, then sends the browser to install it. */
async function finishConnecting(query: URLSearchParams): Promise<Outcome> {
	const code = query.get('code') ?? '';
	const state = query.get('state') ?? '';
	const { requestId } = await finishGitHubConnection(code, state);

	const answer = await waitForGitHubRequest(requestId);
	if (answer.status === 'failed') {
		return { isFailed: true, reason: answer.result };
	}

	const status = await getGitHub();
	if (status.installUrl) {
		window.location.assign(status.installUrl);
	}
	return { isFailed: false };
}

/** Reads the new installation through the agent, then opens Settings. */
async function finishInstalling(): Promise<Outcome> {
	const { requestId } = await refreshGitHub();

	const answer = await waitForGitHubRequest(requestId);
	if (answer.status === 'failed') {
		return { isFailed: true, reason: answer.result };
	}

	navigate('/settings');
	return { isFailed: false };
}
