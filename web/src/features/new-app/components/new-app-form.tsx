import { type SubmitEvent, useState } from 'react';

import { ApiError, createApp, errorMessage, lookUpRepository, type NewApp } from '../../../lib/utils/api';
import { TextField } from './text-field';

type Field = keyof NewApp;
type FieldErrors = Partial<Record<Field, string>>;

const emptyApp: NewApp = { repository: '', branch: '', name: '', rootDirectory: '' };

/**
 * Creates an app and starts its first deploy. Leaving the repository field
 * looks the repository up and fills in the branch and name the admin hasn't
 * typed yet. The server checks every field again.
 */
export function NewAppForm({ onCreated }: { onCreated: (name: string) => void }) {
	const [newApp, setNewApp] = useState<NewApp>(emptyApp);
	const [fieldErrors, setFieldErrors] = useState<FieldErrors>({});
	const [formError, setFormError] = useState('');
	const [isSubmitting, setIsSubmitting] = useState(false);

	const setField = (field: Field) => (value: string) => setNewApp((current) => ({ ...current, [field]: value }));

	const fillFromRepository = async () => {
		const repositoryURL = newApp.repository.trim();
		if (repositoryURL === '') {
			return;
		}

		try {
			const lookup = await lookUpRepository(repositoryURL);
			setFieldErrors((current) => ({ ...current, repository: undefined }));
			setNewApp((current) => ({
				...current,
				repository: lookup.repository,
				branch: current.branch || lookup.defaultBranch,
				name: current.name || lookup.suggestedName,
			}));
		} catch (error) {
			showError(error);
		}
	};

	const showError = (error: unknown) => {
		const isFieldError = error instanceof ApiError && error.field !== '';
		if (isFieldError) {
			setFieldErrors({ [error.field]: error.message });
			return;
		}

		setFormError(errorMessage(error));
	};

	const submit = async (event: SubmitEvent<HTMLFormElement>) => {
		event.preventDefault();
		setIsSubmitting(true);
		setFieldErrors({});
		setFormError('');

		try {
			onCreated(await createApp(newApp));
		} catch (error) {
			showError(error);
		} finally {
			setIsSubmitting(false);
		}
	};

	return (
		<form onSubmit={submit}>
			<TextField
				id="repository"
				label="GitHub repository"
				placeholder="https://github.com/owner/repository"
				hint="Only public repositories for now."
				isRequired
				value={newApp.repository}
				onChange={setField('repository')}
				onBlur={fillFromRepository}
				error={fieldErrors.repository ?? ''}
			/>
			<TextField
				id="branch"
				label="Branch"
				hint="Tero deploys the latest commit of this branch."
				isRequired
				value={newApp.branch}
				onChange={setField('branch')}
				error={fieldErrors.branch ?? ''}
			/>
			<TextField
				id="name"
				label="App name"
				hint="Lowercase letters, digits and hyphens. It becomes part of the app's address and can't be changed later."
				isRequired
				value={newApp.name}
				onChange={setField('name')}
				error={fieldErrors.name ?? ''}
			/>
			<TextField
				id="rootDirectory"
				label="Folder in the repository (optional)"
				hint="Leave empty unless the app lives in a subfolder, such as apps/api."
				value={newApp.rootDirectory}
				onChange={setField('rootDirectory')}
				error={fieldErrors.rootDirectory ?? ''}
			/>
			{formError && <p role="alert">{formError}</p>}
			<button type="submit" disabled={isSubmitting}>
				{isSubmitting ? 'Checking the repository…' : 'Create and deploy'}
			</button>
		</form>
	);
}
