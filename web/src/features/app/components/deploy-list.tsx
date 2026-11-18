import type { Deploy } from '../../../lib/utils/api';
import type { Messages } from '../../../locales/en';
import { describeFailedStep, describeState, stateTone } from '../../../lib/utils/deploy-states';
import { useFormatTime, useMessages } from '../../../lib/utils/locale';

type DeployListProps = {
	deploys: Deploy[];
	shownDeployID: number | undefined;
	onSelect: (deployID: number) => void;
};

export function DeployList({ deploys, shownDeployID, onSelect }: DeployListProps) {
	const messages = useMessages().app.deploys;

	if (deploys.length === 0) {
		return <p className="muted">{messages.empty}</p>;
	}

	return (
		<ul className="t-rows plain" aria-label={messages.caption}>
			{deploys.map((deploy) => (
				<DeployRow key={deploy.id} deploy={deploy} isShown={deploy.id === shownDeployID} onSelect={onSelect} />
			))}
		</ul>
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
	const hasCommit = deploy.commitSha !== '';
	const isFailed = deploy.state === 'failed';
	const stateText = isFailed
		? `${describeState(deploy.state, messages.deployStates)} ${describeFailedStep(deploy.failedStep, messages.deployStates)}`.trim()
		: describeState(deploy.state, messages.deployStates);

	return (
		<li className={isShown ? 't-row sel' : 't-row'}>
			<span className="name">
				{hasCommit ? (
					<span>
						<span className="mono-s">{deploy.commitSha.slice(0, 12)}</span> {deploy.commitMessage}
					</span>
				) : (
					messages.app.deploys.commitNotFetched
				)}
			</span>
			<span className={`t-state ${stateTone(deploy.state)}`}>{stateText}</span>
			<div className="meta row">
				<span>
					<time dateTime={queuedAt.toISOString()}>{formatTime(queuedAt)}</time> · <span>{describeTrigger(deploy, messages.app.deploys.triggers)}</span>
				</span>
				<button className="t-btn secondary sm" type="button" onClick={() => onSelect(deploy.id)} aria-pressed={isShown}>
					{messages.app.deploys.showLog}
				</button>
				{isFailed && deploy.reason && <p>{deploy.reason}</p>}
			</div>
		</li>
	);
}

/** What queued a deploy, in words, naming the pusher of a push. */
function describeTrigger(deploy: Deploy, messages: Messages['app']['deploys']['triggers']): string {
	if (deploy.trigger === 'push') {
		return deploy.pushedBy ? messages.pushBy(deploy.pushedBy) : messages.push;
	}

	return messages[deploy.trigger];
}
