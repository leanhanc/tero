import { type SubmitEvent, useState } from 'react';

import { TextField } from '../../../components/text-field';
import { errorMessage, setDisplayName } from '../../../lib/utils/api';
import { useMessages } from '../../../lib/utils/locale';

type DisplayNameSettingProps = {
	appName: string;
	displayName: string;
	onChange: () => void;
};

/** Renames the app in the dashboard. Its address stays the same. */
export function DisplayNameSetting({ appName, displayName, onChange }: DisplayNameSettingProps) {
	const messages = useMessages();
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
			setError(errorMessage(saveError, messages.errors));
		} finally {
			setIsSaving(false);
		}
	};

	return (
		<form aria-label={messages.app.displayName.formLabel} onSubmit={save}>
			<TextField
				id="display-name"
				label={messages.app.displayName.label}
				hint={messages.app.displayName.hint}
				isRequired
				value={draft}
				onChange={setDraft}
				error={error}
			/>
			<button type="submit" disabled={isSaving || isUnchanged}>
				{isSaving ? messages.common.saving : messages.app.displayName.submit}
			</button>
		</form>
	);
}
