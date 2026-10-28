import { type SubmitEvent, useState } from 'react';

import { AddressHelp } from '../../../components/address-help';
import { ApiError, createApp, errorMessage, type GitHubRepository, lookUpRepository, type NewApp } from '../../../lib/utils/api';
import { toAddressLabel } from '../../../lib/utils/app-address';
import { useMessages } from '../../../lib/utils/locale';
import { TextField } from '../../../components/text-field';
import { GitHubRepositoryPicker } from './github-repository-picker';

type Field = Exclude<keyof NewApp, 'githubRepositoryId'>;
type FieldErrors = Partial<Record<Field, string>>;

const emptyApp: NewApp = { repository: '', branch: '', displayName: '', name: '', rootDirectory: '' };

/**
 * Creates an app and starts its first deploy, from a public repository's URL
 * or a repository the GitHub App can read. Leaving the repository field, or
 * picking a repository, fills in the branch and name the admin hasn't typed
 * yet. The address follows the name until the admin edits it. The server
 * checks every field again.
 */
export function NewAppForm({ onCreated }: { onCreated: (name: string) => void }) {
	const messages = useMessages();
	const formMessages = messages.newApp;
	const [newApp, setNewApp] = useState<NewApp>(emptyApp);
	const [isAddressEdited, setIsAddressEdited] = useState(false);
	const [appDomain, setAppDomain] = useState('');
	const [fieldErrors, setFieldErrors] = useState<FieldErrors>({});
	const [formError, setFormError] = useState('');
	const [isSubmitting, setIsSubmitting] = useState(false);

	const setField = (field: Field) => (value: string) => setNewApp((current) => ({ ...current, [field]: value }));

	const setDisplayName = (displayName: string) =>
		setNewApp((current) => ({
			...current,
			displayName,
			name: isAddressEdited ? current.name : toAddressLabel(displayName),
		}));

	// Clearing the address hands it back to the name.
	const setAddress = (name: string) => {
		setIsAddressEdited(name !== '');
		setField('name')(name);
	};

	// Typing a URL replaces a picked repository.
	const setRepositoryURL = (repository: string) =>
		setNewApp((current) => ({ ...current, repository, githubRepositoryId: undefined }));

	const pickRepository = (picked: GitHubRepository) => {
		setFieldErrors((current) => ({ ...current, repository: undefined }));
		setNewApp((current) => {
			const displayName = current.displayName || picked.name;
			const name = isAddressEdited ? current.name : toAddressLabel(displayName);
			return {
				...current,
				repository: `https://github.com/${picked.owner}/${picked.name}`,
				githubRepositoryId: picked.id,
				branch: current.branch || picked.defaultBranch,
				displayName,
				name,
			};
		});
	};

	const fillFromRepository = async () => {
		const repositoryURL = newApp.repository.trim();
		const isPicked = newApp.githubRepositoryId !== undefined;
		if (repositoryURL === '' || isPicked) {
			return;
		}

		try {
			const lookup = await lookUpRepository(repositoryURL);
			setFieldErrors((current) => ({ ...current, repository: undefined }));
			setAppDomain(lookup.appDomain);
			setNewApp((current) => {
				const displayName = current.displayName || lookup.repositoryName;
				const name = isAddressEdited ? current.name : toAddressLabel(displayName);
				return { ...current, repository: lookup.repository, branch: current.branch || lookup.defaultBranch, displayName, name };
			});
		} catch (error) {
			showError(error);
		}
	};

	const showError = (error: unknown) => {
		const isFieldError = error instanceof ApiError && error.field !== '';
		if (isFieldError) {
			setFieldErrors({ [error.field]: errorMessage(error, messages.errors) });
			return;
		}

		setFormError(errorMessage(error, messages.errors));
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

	const hasAddressPreview = appDomain !== '' && newApp.name !== '';
	const addressPreview = hasAddressPreview ? formMessages.address.preview(`https://${newApp.name}.${appDomain}`) : '';
	const addressHint = `${addressPreview}${formMessages.address.hint}`;

	return (
		<form onSubmit={submit}>
			<TextField
				id="repository"
				label={formMessages.repository.label}
				placeholder={formMessages.repository.placeholder}
				hint={formMessages.repository.hint}
				isRequired
				value={newApp.repository}
				onChange={setRepositoryURL}
				onBlur={fillFromRepository}
				error={fieldErrors.repository ?? ''}
			/>
			<GitHubRepositoryPicker selectedID={newApp.githubRepositoryId} onPick={pickRepository} />
			<TextField
				id="branch"
				label={formMessages.branch.label}
				hint={formMessages.branch.hint}
				isRequired
				value={newApp.branch}
				onChange={setField('branch')}
				error={fieldErrors.branch ?? ''}
			/>
			<TextField
				id="displayName"
				label={formMessages.displayName.label}
				hint={formMessages.displayName.hint}
				isRequired
				value={newApp.displayName}
				onChange={setDisplayName}
				error={fieldErrors.displayName ?? ''}
			/>
			<TextField
				id="name"
				label={formMessages.address.label}
				hint={addressHint}
				isRequired
				value={newApp.name}
				onChange={setAddress}
				error={fieldErrors.name ?? ''}
			/>
			<AddressHelp address={appDomain} />
			<TextField
				id="rootDirectory"
				label={formMessages.rootDirectory.label}
				hint={formMessages.rootDirectory.hint}
				value={newApp.rootDirectory}
				onChange={setField('rootDirectory')}
				error={fieldErrors.rootDirectory ?? ''}
			/>
			{formError && <p role="alert">{formError}</p>}
			<button type="submit" disabled={isSubmitting}>
				{isSubmitting ? formMessages.submitting : formMessages.submit}
			</button>
		</form>
	);
}
