import type { Deploy } from '../../../lib/utils/api';
import { describeFailedStep, describeState } from '../../../lib/utils/deploy-states';
import { useFormatTime, useMessages } from '../../../lib/utils/locale';

type DeployListProps = {
	deploys: Deploy[];
	shownDeployID: number | undefined;
	onSelect: (deployID: number) => void;
};

export function DeployList({ deploys, shownDeployID, onSelect }: DeployListProps) {
	const messages = useMessages().app.deploys;

	if (deploys.length === 0) {
		return <p>{messages.empty}</p>;
	}

	return (
		<table>
			<caption>{messages.caption}</caption>
			<thead>
				<tr>
					<th scope="col">{messages.startedColumn}</th>
					<th scope="col">{messages.commitColumn}</th>
					<th scope="col">{messages.stateColumn}</th>
					<th scope="col">{messages.logColumn}</th>
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
	const messages = useMessages();
	const formatTime = useFormatTime();
	const queuedAt = new Date(deploy.queuedAt * 1000);
	const commit = deploy.commitSha ? `${deploy.commitSha.slice(0, 12)} ${deploy.commitMessage}` : messages.app.deploys.commitNotFetched;
	const isFailed = deploy.state === 'failed';
	const stateText = isFailed
		? `${describeState(deploy.state, messages.deployStates)} ${describeFailedStep(deploy.failedStep, messages.deployStates)}`.trim()
		: describeState(deploy.state, messages.deployStates);

	return (
		<tr>
			<td>
				<time dateTime={queuedAt.toISOString()}>{formatTime(queuedAt)}</time>
			</td>
			<td>{commit}</td>
			<td>
				{stateText}
				{isFailed && deploy.reason && <p>{deploy.reason}</p>}
			</td>
			<td>
				<button type="button" onClick={() => onSelect(deploy.id)} aria-pressed={isShown}>
					{messages.app.deploys.showLog}
				</button>
			</td>
		</tr>
	);
}
