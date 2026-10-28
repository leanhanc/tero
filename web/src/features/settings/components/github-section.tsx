import { type SubmitEvent, useCallback, useEffect, useState } from 'react';

import { TextField } from '../../../components/text-field';
import {
	beginGitHubConnection,
	disconnectGitHub,
	errorMessage,
	type GitHubStatus,
	getGitHub,
	refreshGitHub,
} from '../../../lib/utils/api';
import { postManifest, waitForGitHubRequest } from '../../../lib/utils/github';
import { useMessages } from '../../../lib/utils/locale';
import type { Confirmation } from '../settings-page';

type LoadedGitHub = { status: GitHubStatus } | { error: string } | null;

/**
 * Connects this server to GitHub through a GitHub App of its own, shows
 * where it is installed, and disconnects it. Connecting and disconnecting
 * ask the admin to confirm first.
 */
export function GitHubSection({ confirmation }: { confirmation: Confirmation }) {
	const messages = useMessages();
	const [loaded, reload] = useGitHub(messages.errors);

	return (
		<section aria-labelledby="github-heading">
			<h2 id="github-heading">{messages.settings.github.heading}</h2>
			<GitHubContent loaded={loaded} confirmation={confirmation} onChange={reload} />
		</section>
	);
}

type GitHubContentProps = {
	loaded: LoadedGitHub;
	confirmation: Confirmation;
	onChange: () => void;
};

function GitHubContent({ loaded, confirmation, onChange }: GitHubContentProps) {
	const messages = useMessages();

	if (loaded === null) {
		return <p>{messages.common.loading}</p>;
	}

	if ('error' in loaded) {
		return <p role="alert">{loaded.error}</p>;
	}

	if (!loaded.status.app.isConnected) {
		return <ConnectForm confirmation={confirmation} />;
	}

	return <ConnectedApp status={loaded.status} confirmation={confirmation} onChange={onChange} />;
}

function ConnectForm({ confirmation }: { confirmation: Confirmation }) {
	const messages = useMessages().settings.github;
	const errorMessages = useMessages().errors;
	const [isOrganization, setIsOrganization] = useState(false);
	const [organization, setOrganization] = useState('');
	const [error, setError] = useState('');

	const connect = async (event: SubmitEvent<HTMLFormElement>) => {
		event.preventDefault();
		setError('');
		const owner = isOrganization ? organization.trim() : '';

		try {
			await confirmation.confirmThen(async () => postManifest(await beginGitHubConnection(owner)));
		} catch (failure) {
			setError(errorMessage(failure, errorMessages));
		}
	};

	return (
		<form onSubmit={connect}>
			<p>{messages.intro}</p>
			<fieldset>
				<legend>{messages.ownerLegend}</legend>
				<input
					id="github-owner-personal"
					type="radio"
					name="github-owner"
					checked={!isOrganization}
					onChange={() => setIsOrganization(false)}
				/>
				<label htmlFor="github-owner-personal">{messages.personal}</label>
				<input
					id="github-owner-organization"
					type="radio"
					name="github-owner"
					checked={isOrganization}
					onChange={() => setIsOrganization(true)}
				/>
				<label htmlFor="github-owner-organization">{messages.organization}</label>
			</fieldset>
			{isOrganization && (
				<TextField
					id="github-organization"
					label={messages.organizationName}
					hint={messages.organizationHint}
					isRequired
					value={organization}
					onChange={setOrganization}
					error=""
				/>
			)}
			{error && <p role="alert">{error}</p>}
			<button type="submit" disabled={confirmation.isAwaiting}>
				{messages.connect}
			</button>
		</form>
	);
}

type ConnectedAppProps = {
	status: GitHubStatus;
	confirmation: Confirmation;
	onChange: () => void;
};

function ConnectedApp({ status, confirmation, onChange }: ConnectedAppProps) {
	const messages = useMessages().settings.github;
	const errorMessages = useMessages().errors;
	const [error, setError] = useState('');
	const [isRefreshing, setIsRefreshing] = useState(false);

	const refresh = async () => {
		setError('');
		setIsRefreshing(true);
		try {
			const { requestId } = await refreshGitHub();
			await waitForGitHubRequest(requestId);
			onChange();
		} catch (failure) {
			setError(errorMessage(failure, errorMessages));
		} finally {
			setIsRefreshing(false);
		}
	};

	const disconnect = async () => {
		setError('');
		try {
			await confirmation.confirmThen(disconnectGitHub);
		} catch (failure) {
			setError(errorMessage(failure, errorMessages));
		}
		onChange();
	};

	return (
		<>
			<p>{messages.connectedAs(status.app.slug ?? '', status.app.owner ?? '')}</p>
			<Installation status={status} />
			<p>
				<a href={status.installUrl} rel="noreferrer">
					{messages.chooseRepositories}
				</a>
			</p>
			<button type="button" onClick={refresh} disabled={isRefreshing}>
				{isRefreshing ? messages.refreshing : messages.refresh}
			</button>
			<button type="button" onClick={disconnect} disabled={confirmation.isAwaiting}>
				{messages.disconnect}
			</button>
			<p>
				{messages.deleteHint}{' '}
				<a href={status.settingsUrl} rel="noreferrer">
					{messages.appOnGitHub}
				</a>
			</p>
			{error && <p role="alert">{error}</p>}
		</>
	);
}

function Installation({ status }: { status: GitHubStatus }) {
	const messages = useMessages().settings.github;
	const { installation, repositories } = status;

	if (installation === null) {
		return <p>{messages.notInstalled}</p>;
	}

	if (installation.isSuspended) {
		return <p role="alert">{messages.suspended(installation.account)}</p>;
	}

	return (
		<>
			<p>{messages.installedOn(installation.account)}</p>
			{repositories.length === 0 ? (
				<p>{messages.noRepositories}</p>
			) : (
				<ul aria-label={messages.repositoriesLabel}>
					{repositories.map((repository) => (
						<li key={repository.id}>
							{repository.owner}/{repository.name}
							{repository.isPrivate && ` (${messages.private})`}
						</li>
					))}
				</ul>
			)}
		</>
	);
}

/** Loads the GitHub connection, again whenever reload is called. */
function useGitHub(errorMessages: Parameters<typeof errorMessage>[1]): [LoadedGitHub, () => void] {
	const [loaded, setLoaded] = useState<LoadedGitHub>(null);
	const [reloadCount, setReloadCount] = useState(0);

	useEffect(() => {
		let isCurrent = true;

		getGitHub()
			.then((status) => isCurrent && setLoaded({ status }))
			.catch((error: unknown) => isCurrent && setLoaded({ error: errorMessage(error, errorMessages) }));

		return () => {
			isCurrent = false;
		};
	}, [reloadCount, errorMessages]);

	const reload = useCallback(() => setReloadCount((count) => count + 1), []);
	return [loaded, reload];
}
