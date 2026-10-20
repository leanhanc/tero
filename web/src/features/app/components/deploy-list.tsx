import type { Deploy } from '../../../lib/utils/api';
import { describeFailedStep, describeState } from '../../../lib/utils/deploy-states';

type DeployListProps = {
	deploys: Deploy[];
	shownDeployID: number | undefined;
	onSelect: (deployID: number) => void;
};

export function DeployList({ deploys, shownDeployID, onSelect }: DeployListProps) {
	if (deploys.length === 0) {
		return <p>No deploys yet.</p>;
	}

	return (
		<table>
			<caption>Deploys, newest first</caption>
			<thead>
				<tr>
					<th scope="col">Started</th>
					<th scope="col">Commit</th>
					<th scope="col">State</th>
					<th scope="col">Log</th>
				</tr>
			</thead>
			<tbody>
				{deploys.map((deploy) => (
					<DeployRow key={deploy.id} deploy={deploy} isShown={deploy.id === shownDeployID} onSelect={onSelect} />
				))}
			</tbody>
		</table>
	);
}

type DeployRowProps = {
	deploy: Deploy;
	isShown: boolean;
	onSelect: (deployID: number) => void;
};

function DeployRow({ deploy, isShown, onSelect }: DeployRowProps) {
	const queuedAt = new Date(deploy.queuedAt * 1000);
	const commit = deploy.commitSha ? `${deploy.commitSha.slice(0, 12)} ${deploy.commitMessage}` : 'Not fetched yet';
	const isFailed = deploy.state === 'failed';
	const stateText = isFailed
		? `${describeState(deploy.state)} ${describeFailedStep(deploy.failedStep)}`.trim()
		: describeState(deploy.state);

	return (
		<tr>
			<td>
				<time dateTime={queuedAt.toISOString()}>{queuedAt.toLocaleString()}</time>
			</td>
			<td>{commit}</td>
			<td>
				{stateText}
				{isFailed && deploy.reason && <p>{deploy.reason}</p>}
			</td>
			<td>
				<button type="button" onClick={() => onSelect(deploy.id)} aria-pressed={isShown}>
					Show log
				</button>
			</td>
		</tr>
	);
}
