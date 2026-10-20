import { useState } from 'react';

import { errorMessage, setRunTogether } from '../../../lib/utils/api';

type RunTogetherSettingProps = {
	appName: string;
	runTogether: boolean;
	onChange: () => void;
};

export function RunTogetherSetting({ appName, runTogether, onChange }: RunTogetherSettingProps) {
	const [error, setError] = useState('');

	const toggle = async (isChecked: boolean) => {
		setError('');
		try {
			await setRunTogether(appName, isChecked);
			onChange();
		} catch (settingError) {
			setError(errorMessage(settingError));
		}
	};

	return (
		<section aria-label="Deploy settings">
			<input
				id="run-together"
				type="checkbox"
				checked={runTogether}
				onChange={(event) => toggle(event.target.checked)}
				aria-describedby="run-together-hint"
			/>
			<label htmlFor="run-together">Run old and new releases together during deploys</label>
			<p id="run-together-hint">
				When on, the new release starts next to the old one and takes over once it answers. Turn it off for apps
				that can't run twice at once: the old release then stops first, so the app is briefly down. Either way, if
				the new release fails, the old one keeps serving.
			</p>
			{error && <p role="alert">{error}</p>}
		</section>
	);
}
