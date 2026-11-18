import { useEffect, useState } from 'react';

import { AddressHelp } from '../../../components/address-help';
import { DeployStateBadge } from '../../../components/deploy-state-badge';
import { LogIcon, PlusIcon } from '../../../components/icons';
import { InternalLink } from '../../../components/internal-link';
import { type AppSummary, errorMessage, listApps } from '../../../lib/utils/api';
import { useMessages } from '../../../lib/utils/locale';
import { NextStep } from './next-step';

type LoadedApps = { apps: AppSummary[] } | { error: string } | null;

/** The home page: every app in a row, and what to do next. */
export function AppList({ heading }: { heading: string }) {
	const messages = useMessages();
	const [loaded, setLoaded] = useState<LoadedApps>(null);

	useEffect(() => {
		let isCurrent = true;
		listApps()
			.then((apps) => isCurrent && setLoaded({ apps }))
			.catch((error: unknown) => isCurrent && setLoaded({ error: errorMessage(error, messages.errors) }));

		return () => {
			isCurrent = false;
		};
	}, [messages]);

	const hasApps = loaded !== null && 'apps' in loaded && loaded.apps.length > 0;

	return (
		<>
			<div className="ph">
				<h1>{heading}</h1>
				{hasApps && <DeployNewLink />}
			</div>
			<section className="stack" aria-label={messages.home.apps.label}>
				<AppListContent loaded={loaded} />
			</section>
		</>
	);
}

function DeployNewLink() {
	const messages = useMessages();

	return (
		<InternalLink to="/apps/new" className="t-btn primary">
			<PlusIcon />
			{messages.home.apps.deployNew}
		</InternalLink>
	);
}

function AppListContent({ loaded }: { loaded: LoadedApps }) {
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

	if (loaded.apps.length === 0) {
		return <NoApps />;
	}

	const firstAddress = loaded.apps[0]?.address ?? '';
	const failedApp = loaded.apps.find((app) => app.latestState === 'failed');

	return (
		<>
			<ul className="t-rows plain">
				{loaded.apps.map((app) => (
					<AppRow key={app.name} app={app} />
				))}
			</ul>
			<div className="cap">
				{messages.home.apps.addressNote} <AddressHelp address={firstAddress} />
			</div>
			{failedApp && <FixAppStep app={failedApp} />}
		</>
	);
}

function AppRow({ app }: { app: AppSummary }) {
	const messages = useMessages();
	const hasPendingChanges = app.pendingChanges > 0;

	return (
		<li className="t-row">
			<InternalLink to={`/apps/${app.name}`} className="name">
				{app.displayName}
			</InternalLink>
			<DeployStateBadge state={app.latestState} />
			<a className="url" href={app.address} rel="noreferrer" target="_blank">
				{app.address}
			</a>
			{hasPendingChanges && <span className="meta">{messages.app.pendingChanges.count(app.pendingChanges)}</span>}
		</li>
	);
}

function NoApps() {
	const messages = useMessages().home;

	return (
		<>
			<div className="t-empty">
				<b>{messages.apps.emptyHeading}</b>
				<p>{messages.apps.emptyBody}</p>
				<DeployNewLink />
			</div>
			<NextStep heading={messages.nextStep.firstHeading}>
				<p>{messages.nextStep.firstBody}</p>
				<div className="row">
					<InternalLink to="/settings" className="t-btn quiet">
						{messages.nextStep.connectGitHub}
					</InternalLink>
				</div>
			</NextStep>
		</>
	);
}

function FixAppStep({ app }: { app: AppSummary }) {
	const messages = useMessages().home.nextStep;

	return (
		<NextStep heading={messages.fixHeading(app.displayName)}>
			<p>{messages.fixBody}</p>
			<div className="row">
				<InternalLink to={`/apps/${app.name}?tab=deploys`} className="t-btn primary">
					<LogIcon />
					{messages.viewLog}
				</InternalLink>
			</div>
		</NextStep>
	);
}
