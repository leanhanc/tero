import { useState } from 'react';

import { errorMessage, redeploy } from '../../../lib/utils/api';
import { useMessages } from '../../../lib/utils/locale';
import type { Messages } from '../../../locales/en';

type RedeployButtonProps = {
	appName: string;
	hasDeployRunning: boolean;
	hasDeployQueued: boolean;
	onRedeploy: () => void;
};

/**
 * Deploys the branch's latest commit. While a deploy runs, it queues one
 * after it; while one is queued, there is nothing to add, since the queued
 * deploy builds the latest commit when it starts.
 */
export function RedeployButton({ appName, hasDeployRunning, hasDeployQueued, onRedeploy }: RedeployButtonProps) {
	const allMessages = useMessages();
	const messages = allMessages.app.redeploy;
	const [error, setError] = useState('');
	const [isSubmitting, setIsSubmitting] = useState(false);

	const startRedeploy = async () => {
		setIsSubmitting(true);
		setError('');

		try {
			await redeploy(appName);
			onRedeploy();
		} catch (redeployError) {
			setError(errorMessage(redeployError, allMessages.errors));
		} finally {
			setIsSubmitting(false);
		}
	};

	const label = hasDeployQueued ? messages.queued : messages.submit;
	const hint = redeployHint(hasDeployRunning, hasDeployQueued, messages);

	return (
		<section aria-label={messages.label}>
			<button type="button" onClick={startRedeploy} disabled={hasDeployQueued || isSubmitting} aria-describedby="redeploy-hint">
				{label}
			</button>
			<p id="redeploy-hint">{hint}</p>
			{error && <p role="alert">{error}</p>}
		</section>
	);
}

function redeployHint(
	hasDeployRunning: boolean,
	hasDeployQueued: boolean,
	messages: Messages['app']['redeploy'],
): string {
	if (hasDeployQueued) {
		return messages.queuedHint;
	}

	if (hasDeployRunning) {
		return messages.runningHint;
	}

	return messages.hint;
}
