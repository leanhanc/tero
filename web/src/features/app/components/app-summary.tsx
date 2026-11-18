import { BranchIcon, CommitIcon, FolderIcon, GitHubIcon } from '../../../components/icons';
import type { AppDetail, Deploy } from '../../../lib/utils/api';
import { useMessages } from '../../../lib/utils/locale';
import { BuildLog } from './build-log';

type AppSummaryProps = {
	app: AppDetail;
	latestDeploy: Deploy | undefined;
	onShowDeploys: () => void;
};

/** Where the app comes from, what is live, and the latest deploy's log. */
export function AppSummary({ app, latestDeploy, onShowDeploys }: AppSummaryProps) {
	const allMessages = useMessages();
	const messages = allMessages.app;

	return (
		<>
			<SourceFacts app={app} />
			<h2 className="h2">{messages.latestDeploy}</h2>
			{latestDeploy ? (
				<BuildLog
					key={latestDeploy.id}
					deploy={latestDeploy}
					action={
						<button className="t-btn quiet sm" type="button" onClick={onShowDeploys}>
							{messages.allDeploys}
						</button>
					}
				/>
			) : (
				<p className="muted">{messages.deploys.empty}</p>
			)}
		</>
	);
}

function SourceFacts({ app }: { app: AppDetail }) {
	const messages = useMessages().app.summary;
	const liveDeploy = app.deploys.find((deploy) => deploy.state === 'live');
	const liveCommit = liveDeploy ? `${liveDeploy.commitSha.slice(0, 12)} ${liveDeploy.commitMessage}` : messages.nothingLive;

	return (
		<dl className="facts">
			<div>
				<dt>
					<GitHubIcon />
					{messages.repository}
				</dt>
				<dd className="mono-s">{app.repository}</dd>
			</div>
			<div>
				<dt>
					<BranchIcon />
					{messages.branch}
				</dt>
				<dd className="mono-s">{app.branch}</dd>
			</div>
			<div>
				<dt>
					<FolderIcon />
					{messages.folder}
				</dt>
				<dd>{app.rootDirectory || messages.repositoryRoot}</dd>
			</div>
			<div>
				<dt>
					<CommitIcon />
					{messages.liveCommit}
				</dt>
				<dd>{liveCommit}</dd>
			</div>
		</dl>
	);
}
