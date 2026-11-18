import { type SubmitEvent, useCallback, useEffect, useState } from 'react';

import { ExternalLinkIcon, GitHubIcon, LockIcon, RedeployIcon } from '../../../components/icons';
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
		<section className="card" aria-labelledby="github-heading">
			<h2 className="h2" id="github-heading">
				{messages.settings.github.heading}
			</h2>
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
		return <p className="muted">{messages.common.loading}</p>;
	}

	if ('error' in loaded) {
		return (
			<p className="t-error" role="alert">
				{loaded.error}
			</p>
		);
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
		<form className="stack" onSubmit={connect}>
			<p className="muted">{messages.intro}</p>
			<fieldset className="fs">
				<legend>{messages.ownerLegend}</legend>
				<label className="t-check">
					<input type="radio" name="github-owner" checked={!isOrganization} onChange={() => setIsOrganization(false)} />
					<b>{messages.personal}</b>
				</label>
				<label className="t-check">
					<input type="radio" name="github-owner" checked={isOrganization} onChange={() => setIsOrganization(true)} />
					<b>{messages.organization}</b>
				</label>
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
			{error && (
				<p className="t-error" role="alert">
					{error}
				</p>
			)}
			<div className="row">
				<button className="t-btn primary" type="submit" disabled={confirmation.isAwaiting}>
					<GitHubIcon />
					{messages.connect}
				</button>
			</div>
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
			<div className="t-note ok">
				<span className="t-state ok" aria-hidden="true" />
				<div>
					<p>{messages.connectedAs(status.app.slug ?? '', status.app.owner ?? '')}</p>
				</div>
			</div>
			<Installation status={status} />
			<div className="row">
				<a className="t-btn secondary" href={status.installUrl} rel="noreferrer">
					<ExternalLinkIcon />
					{messages.chooseRepositories}
				</a>
				<button className="t-btn quiet" type="button" onClick={refresh} disabled={isRefreshing}>
					<RedeployIcon />
					{isRefreshing ? messages.refreshing : messages.refresh}
				</button>
			</div>
			{error && (
				<p className="t-error" role="alert">
					{error}
				</p>
			)}
			<section className="card danger" aria-labelledby="github-disconnect-heading">
				<h3 className="h3" id="github-disconnect-heading">
					{messages.disconnectHeading}
				</h3>
				<p className="muted">
					{messages.deleteHint}{' '}
					<a className="t-link" href={status.settingsUrl} rel="noreferrer">
						{messages.appOnGitHub}
					</a>
				</p>
				<div className="row">
					<button className="t-btn danger" type="button" onClick={disconnect} disabled={confirmation.isAwaiting}>
						{messages.disconnect}
					</button>
				</div>
			</section>
		</>
	);
}

function Installation({ status }: { status: GitHubStatus }) {
	const messages = useMessages().settings.github;
	const { installation, repositories } = status;

	if (installation === null) {
		return <p className="muted">{messages.notInstalled}</p>;
	}

	if (installation.isSuspended) {
		return (
			<p className="t-error" role="alert">
				{messages.suspended(installation.account)}
			</p>
		);
	}

	return (
		<>
			<p>{messages.installedOn(installation.account)}</p>
			{repositories.length === 0 ? (
				<p className="muted">{messages.noRepositories}</p>
			) : (
				<ul className="t-rows plain" aria-label={messages.repositoriesLabel}>
					{repositories.map((repository) => (
						<li key={repository.id} className="t-row">
							<span className="name mono-s">
								{repository.owner}/{repository.name}
								{repository.isPrivate && (
									<span className="t-chip">
										<LockIcon />
										{messages.private}
									</span>
								)}
							</span>
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
