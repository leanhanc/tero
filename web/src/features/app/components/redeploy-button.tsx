import { useState } from 'react';

import { errorMessage, redeploy } from '../../../lib/utils/api';

type RedeployButtonProps = {
	appName: string;
	hasDeployInProgress: boolean;
	onRedeploy: () => void;
};

/** Deploys the branch's latest commit. One deploy at a time per app. */
export function RedeployButton({ appName, hasDeployInProgress, onRedeploy }: RedeployButtonProps) {
	const [error, setError] = useState('');
	const [isSubmitting, setIsSubmitting] = useState(false);

	const startRedeploy = async () => {
		setIsSubmitting(true);
		setError('');

		try {
			await redeploy(appName);
			onRedeploy();
		} catch (redeployError) {
			setError(errorMessage(redeployError));
		} finally {
			setIsSubmitting(false);
		}
	};

	return (
		<section aria-label="Redeploy">
			<button type="button" onClick={startRedeploy} disabled={hasDeployInProgress || isSubmitting} aria-describedby="redeploy-hint">
				Redeploy
			</button>
			<p id="redeploy-hint">
				{hasDeployInProgress ? 'A deploy is in progress.' : "Builds and deploys the branch's latest commit."}
			</p>
			{error && <p role="alert">{error}</p>}
		</section>
	);
}
