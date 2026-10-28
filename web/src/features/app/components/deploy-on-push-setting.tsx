import { useState } from 'react';

import { errorMessage, setDeployOnPush } from '../../../lib/utils/api';
import { useMessages } from '../../../lib/utils/locale';

type DeployOnPushSettingProps = {
	appName: string;
	deployOnPush: boolean;
	onChange: () => void;
};

export function DeployOnPushSetting({ appName, deployOnPush, onChange }: DeployOnPushSettingProps) {
	const messages = useMessages();
	const [error, setError] = useState('');

	const toggle = async (isChecked: boolean) => {
		setError('');
		try {
			await setDeployOnPush(appName, isChecked);
			onChange();
		} catch (settingError) {
			setError(errorMessage(settingError, messages.errors));
		}
	};

	return (
		<section aria-label={messages.app.deployOnPush.sectionLabel}>
			<input
				id="deploy-on-push"
				type="checkbox"
				checked={deployOnPush}
				onChange={(event) => toggle(event.target.checked)}
				aria-describedby="deploy-on-push-hint"
			/>
			<label htmlFor="deploy-on-push">{messages.app.deployOnPush.label}</label>
			<p id="deploy-on-push-hint">{messages.app.deployOnPush.hint}</p>
			{error && <p role="alert">{error}</p>}
		</section>
	);
}
