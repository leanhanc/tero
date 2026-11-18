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
		<div className="stack">
			<label className="t-switch">
				<input
					id="deploy-on-push"
					type="checkbox"
					role="switch"
					checked={deployOnPush}
					onChange={(event) => toggle(event.target.checked)}
					aria-labelledby="deploy-on-push-label"
					aria-describedby="deploy-on-push-hint"
				/>
				<b id="deploy-on-push-label">{messages.app.deployOnPush.label}</b>
				<span id="deploy-on-push-hint">{messages.app.deployOnPush.hint}</span>
			</label>
			{error && (
				<p className="t-error" role="alert">
					{error}
				</p>
			)}
		</div>
	);
}
