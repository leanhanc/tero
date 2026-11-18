import { useState } from 'react';

import { errorMessage, setRunTogether } from '../../../lib/utils/api';
import { useMessages } from '../../../lib/utils/locale';

type RunTogetherSettingProps = {
	appName: string;
	runTogether: boolean;
	onChange: () => void;
};

export function RunTogetherSetting({ appName, runTogether, onChange }: RunTogetherSettingProps) {
	const messages = useMessages();
	const [error, setError] = useState('');

	const toggle = async (isChecked: boolean) => {
		setError('');
		try {
			await setRunTogether(appName, isChecked);
			onChange();
		} catch (settingError) {
			setError(errorMessage(settingError, messages.errors));
		}
	};

	return (
		<div className="stack">
			<label className="t-switch">
				<input
					id="run-together"
					type="checkbox"
					role="switch"
					checked={runTogether}
					onChange={(event) => toggle(event.target.checked)}
					aria-labelledby="run-together-label"
					aria-describedby="run-together-hint"
				/>
				<b id="run-together-label">{messages.app.runTogether.label}</b>
				<span id="run-together-hint">{messages.app.runTogether.hint}</span>
			</label>
			{error && (
				<p className="t-error" role="alert">
					{error}
				</p>
			)}
		</div>
	);
}
