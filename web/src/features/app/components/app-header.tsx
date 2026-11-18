import { useState } from 'react';

import { AddressHelp } from '../../../components/address-help';
import { DeployStateBadge } from '../../../components/deploy-state-badge';
import { CopyIcon, ExternalLinkIcon, RedeployIcon } from '../../../components/icons';
import { InternalLink } from '../../../components/internal-link';
import { type AppDetail, errorMessage, redeploy } from '../../../lib/utils/api';
import { useMessages } from '../../../lib/utils/locale';
import type { Messages } from '../../../locales/en';

type AppHeaderProps = {
	app: AppDetail;
	onRedeploy: () => void;
};

/**
 * The top of an app's page, the same on every tab: its name and state, a
 * link to open it, Redeploy, and its address to copy.
 */
export function AppHeader({ app, onRedeploy }: AppHeaderProps) {
	const messages = useMessages();
	const latestState = app.deploys[0]?.state ?? '';

	return (
		<>
			<p className="crumb">
				<InternalLink to="/" className="t-link">
					{messages.home.heading}
				</InternalLink>{' '}
				/ {app.displayName}
			</p>
			<div className="ph">
				<div className="ttl">
					<h1>{app.displayName}</h1>
					<DeployStateBadge state={latestState} />
				</div>
				<RedeployActions app={app} onRedeploy={onRedeploy} />
			</div>
			<div className="row">
				<CopyAddress app={app} />
				<AddressHelp address={app.address} />
			</div>
		</>
	);
}

/**
 * Deploys the branch's latest commit. While a deploy runs, it queues one
 * after it; while one is queued, there is nothing to add, since the queued
 * deploy builds the latest commit when it starts.
 */
function RedeployActions({ app, onRedeploy }: AppHeaderProps) {
	const allMessages = useMessages();
	const messages = allMessages.app.redeploy;
	const [error, setError] = useState('');
	const [isSubmitting, setIsSubmitting] = useState(false);

	const hasDeployQueued = app.deploys.some((deploy) => deploy.state === 'queued');
	const hasDeployRunning = app.deploys.some((deploy) => deploy.state === 'building' || deploy.state === 'starting');
	const label = hasDeployQueued ? messages.queued : messages.submit;
	const hint = redeployHint(hasDeployRunning, hasDeployQueued, messages);

	const startRedeploy = async () => {
		setIsSubmitting(true);
		setError('');

		try {
			await redeploy(app.name);
			onRedeploy();
		} catch (redeployError) {
			setError(errorMessage(redeployError, allMessages.errors));
		} finally {
			setIsSubmitting(false);
		}
	};

	return (
		<section className="stack" aria-label={messages.label}>
			<div className="row">
				<a className="t-btn secondary" href={app.address} rel="noreferrer" target="_blank">
					<ExternalLinkIcon />
					{allMessages.app.openApp}
				</a>
				<button
					className="t-btn primary"
					type="button"
					onClick={startRedeploy}
					disabled={hasDeployQueued || isSubmitting}
					aria-describedby={hint ? 'redeploy-hint' : undefined}
				>
					<RedeployIcon />
					{label}
				</button>
			</div>
			{hint && (
				<p className="cap" id="redeploy-hint">
					{hint}
				</p>
			)}
			{error && (
				<p className="t-error" role="alert">
					{error}
				</p>
			)}
		</section>
	);
}

/** Why Redeploy queues or waits, or nothing when it simply deploys. */
function redeployHint(hasDeployRunning: boolean, hasDeployQueued: boolean, messages: Messages['app']['redeploy']): string {
	if (hasDeployQueued) {
		return messages.queuedHint;
	}

	if (hasDeployRunning) {
		return messages.runningHint;
	}

	return '';
}

// Long enough to read "Copied", short enough that a second copy looks fresh.
const copiedForMs = 2000;

function CopyAddress({ app }: { app: AppDetail }) {
	const messages = useMessages().app;
	const [isCopied, setIsCopied] = useState(false);

	const copy = async () => {
		await navigator.clipboard.writeText(app.address);
		setIsCopied(true);
		window.setTimeout(() => setIsCopied(false), copiedForMs);
	};

	return (
		<div className="t-copy">
			<code>{app.address}</code>
			<button className="t-btn secondary" type="button" aria-label={messages.copyLabel(app.displayName)} onClick={copy}>
				<CopyIcon />
				<span aria-live="polite">{isCopied ? messages.copied : messages.copy}</span>
			</button>
		</div>
	);
}
