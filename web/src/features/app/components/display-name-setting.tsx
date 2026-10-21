import { type SubmitEvent, useState } from 'react';

import { TextField } from '../../../components/text-field';
import { errorMessage, setDisplayName } from '../../../lib/utils/api';

type DisplayNameSettingProps = {
	appName: string;
	displayName: string;
	onChange: () => void;
};

/** Renames the app in the dashboard. Its address stays the same. */
export function DisplayNameSetting({ appName, displayName, onChange }: DisplayNameSettingProps) {
	const [draft, setDraft] = useState(displayName);
	const [error, setError] = useState('');
	const [isSaving, setIsSaving] = useState(false);

	const isUnchanged = draft.trim() === displayName;

	const save = async (event: SubmitEvent<HTMLFormElement>) => {
		event.preventDefault();
		setIsSaving(true);
		setError('');

		try {
			await setDisplayName(appName, draft);
			onChange();
		} catch (saveError) {
			setError(errorMessage(saveError));
		} finally {
			setIsSaving(false);
		}
	};

	return (
		<form aria-label="App name" onSubmit={save}>
			<TextField
				id="display-name"
				label="Name"
				hint="What the dashboard calls the app. Changing it doesn't change the address."
				isRequired
				value={draft}
				onChange={setDraft}
				error={error}
			/>
			<button type="submit" disabled={isSaving || isUnchanged}>
				{isSaving ? 'Saving…' : 'Save name'}
			</button>
		</form>
	);
}
