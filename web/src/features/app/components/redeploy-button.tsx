import { useState } from 'react';

import { errorMessage, redeploy } from '../../../lib/utils/api';
import { useMessages } from '../../../lib/utils/locale';

type RedeployButtonProps = {
	appName: string;
	hasDeployInProgress: boolean;
	onRedeploy: () => void;
};

/** Deploys the branch's latest commit. One deploy at a time per app. */
export function RedeployButton({ appName, hasDeployInProgress, onRedeploy }: RedeployButtonProps) {
	const messages = useMessages();
	const [error, setError] = useState('');
	const [isSubmitting, setIsSubmitting] = useState(false);

	const startRedeploy = async () => {
		setIsSubmitting(true);
		setError('');

		try {
			await redeploy(appName);
			onRedeploy();
		} catch (redeployError) {
			setError(errorMessage(redeployError, messages.errors));
		} finally {
			setIsSubmitting(false);
		}
	};

	return (
		<section aria-label={messages.app.redeploy.label}>
			<button type="button" onClick={startRedeploy} disabled={hasDeployInProgress || isSubmitting} aria-describedby="redeploy-hint">
				{messages.app.redeploy.submit}
			</button>
			<p id="redeploy-hint">
				{hasDeployInProgress ? messages.app.redeploy.inProgress : messages.app.redeploy.hint}
			</p>
			{error && <p role="alert">{error}</p>}
		</section>
	);
}
