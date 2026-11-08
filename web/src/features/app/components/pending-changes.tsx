import { useState } from 'react';

import { type AppDetail, errorMessage, redeployVariables } from '../../../lib/utils/api';
import { useMessages } from '../../../lib/utils/locale';

type PendingChangesProps = {
	app: AppDetail;
	onRedeploy: () => void;
};

/**
 * Saving changes deploys nothing. This says what is waiting and applies it, with
 * a restart when no change reaches a build.
 */
export function PendingChanges({ app, onRedeploy }: PendingChangesProps) {
	const allMessages = useMessages();
	const messages = allMessages.app.pendingChanges;
	const [error, setError] = useState('');
	const [isSubmitting, setIsSubmitting] = useState(false);

	const pendingCount = app.pendingChanges.length;
	if (pendingCount === 0) {
		return null;
	}

	const startRedeploy = async () => {
		setIsSubmitting(true);
		setError('');

		try {
			await redeployVariables(app.name);
			onRedeploy();
		} catch (redeployError) {
			setError(errorMessage(redeployError, allMessages.errors));
		} finally {
			setIsSubmitting(false);
		}
	};

	const hasDeployQueued = app.deploys.some((deploy) => deploy.state === 'queued');
	const hint = app.pendingNeedsBuild ? messages.buildHint : messages.restartHint;

	return (
		<section aria-label={messages.label}>
			<p role="status">{messages.count(pendingCount)}</p>
			<p>{app.pendingChanges.join(', ')}</p>
			<button type="button" onClick={startRedeploy} disabled={hasDeployQueued || isSubmitting} aria-describedby="pending-changes-hint">
				{messages.submit}
			</button>
			<p id="pending-changes-hint">{hint}</p>
			{error && <p role="alert">{error}</p>}
		</section>
	);
}
