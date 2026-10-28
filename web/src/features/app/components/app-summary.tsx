import { AddressHelp } from '../../../components/address-help';
import type { AppDetail } from '../../../lib/utils/api';
import { useMessages } from '../../../lib/utils/locale';

export function AppSummary({ app }: { app: AppDetail }) {
	const { app: appMessages } = useMessages();
	const messages = appMessages.summary;
	const liveDeploy = app.deploys.find((deploy) => deploy.state === 'live');
	const liveCommit = liveDeploy ? `${liveDeploy.commitSha.slice(0, 12)} ${liveDeploy.commitMessage}` : messages.nothingLive;

	return (
		<>
			{app.pollError && <p role="alert">{messages.pollStopped(app.pollError)}</p>}
			<SummaryList app={app} liveCommit={liveCommit} />
		</>
	);
}

function SummaryList({ app, liveCommit }: { app: AppDetail; liveCommit: string }) {
	const messages = useMessages().app.summary;

	return (
		<dl>
			<dt>
				{messages.address} <AddressHelp address={app.address} />
			</dt>
			<dd>
				<a href={app.address} rel="noreferrer" target="_blank">
					{app.address}
				</a>
			</dd>
			<dt>{messages.repository}</dt>
			<dd>{app.repository}</dd>
			<dt>{messages.branch}</dt>
			<dd>{app.branch}</dd>
			<dt>{messages.folder}</dt>
			<dd>{app.rootDirectory || messages.repositoryRoot}</dd>
			<dt>{messages.liveCommit}</dt>
			<dd>{liveCommit}</dd>
		</dl>
	);
}
