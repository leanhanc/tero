import type { AppDetail } from '../../../lib/utils/api';

export function AppSummary({ app }: { app: AppDetail }) {
	const liveDeploy = app.deploys.find((deploy) => deploy.state === 'live');
	const liveCommit = liveDeploy ? `${liveDeploy.commitSha.slice(0, 12)} ${liveDeploy.commitMessage}` : 'Nothing is live yet';

	return (
		<dl>
			<dt>Address</dt>
			<dd>
				<a href={app.address} rel="noreferrer" target="_blank">
					{app.address}
				</a>
			</dd>
			<dt>Repository</dt>
			<dd>{app.repository}</dd>
			<dt>Branch</dt>
			<dd>{app.branch}</dd>
			<dt>Folder</dt>
			<dd>{app.rootDirectory || 'The repository root'}</dd>
			<dt>Live commit</dt>
			<dd>{liveCommit}</dd>
		</dl>
	);
}
