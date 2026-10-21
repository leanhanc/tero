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
		<section aria-label={messages.app.runTogether.sectionLabel}>
			<input
				id="run-together"
				type="checkbox"
				checked={runTogether}
				onChange={(event) => toggle(event.target.checked)}
				aria-describedby="run-together-hint"
			/>
			<label htmlFor="run-together">{messages.app.runTogether.label}</label>
			<p id="run-together-hint">{messages.app.runTogether.hint}</p>
			{error && <p role="alert">{error}</p>}
		</section>
	);
}
