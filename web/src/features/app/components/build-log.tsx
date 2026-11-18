import { type ReactNode, useEffect, useState } from 'react';

import { DeployStateBadge } from '../../../components/deploy-state-badge';
import { type Deploy, deployLogURL } from '../../../lib/utils/api';
import { formatDuration } from '../../../lib/utils/duration';
import { useMessages } from '../../../lib/utils/locale';

type LogStatus = 'streaming' | 'finished' | 'unavailable';

// Some build steps, such as npm install, print nothing for minutes. After this
// long without output the page says the deploy is still running.
const quietAfterMs = 30_000;

/**
 * Streams a deploy's build log from the server and shows it as plain text.
 * React escapes it, so build output can never become markup.
 */
export function BuildLog({ deploy, action }: { deploy: Deploy; action?: ReactNode }) {
	const messages = useMessages().app.buildLog;
	const [log, setLog] = useState('');
	const [status, setStatus] = useState<LogStatus>('streaming');
	const [lastOutputAt, setLastOutputAt] = useState(() => Date.now());

	useEffect(() => {
		const source = new EventSource(deployLogURL(deploy.id));

		// The server sends the whole log again on every connection, so a
		// reconnect after a dropped stream starts over instead of repeating it.
		source.addEventListener('open', () => setLog(''));

		source.addEventListener('log', (event) => {
			const chunk = JSON.parse((event as MessageEvent<string>).data) as string;
			setLog((current) => current + chunk);
			setLastOutputAt(Date.now());
		});
		source.addEventListener('done', () => {
			setStatus('finished');
			source.close();
		});
		source.addEventListener('problem', () => {
			setStatus('unavailable');
			source.close();
		});

		return () => source.close();
	}, [deploy.id]);

	const isStreaming = status === 'streaming';
	const headingID = `build-log-heading-${deploy.id}`;

	return (
		<section className="deploy" aria-labelledby={headingID}>
			<div className="dh">
				<DeployStateBadge state={deploy.state} />
				<h2 id={headingID}>{messages.heading(deploy.id)}</h2>
				<div className="meta">{isStreaming && <DeployProgress queuedAt={deploy.queuedAt} lastOutputAt={lastOutputAt} />}</div>
				{action}
			</div>
			{status === 'unavailable' && (
				<div className="t-note err" role="alert">
					<span className="t-state err" aria-hidden="true" />
					<div>
						<p>{messages.unavailable}</p>
					</div>
				</div>
			)}
			<pre className="log">
				{log || messages.empty}
				{isStreaming && (
					<span className="live" aria-live="polite">
						{'\n'}
						{messages.streaming}
					</span>
				)}
			</pre>
		</section>
	);
}

type DeployProgressProps = {
	/** Seconds since the epoch, as the API sends it. */
	queuedAt: number;
	/** Milliseconds since the epoch. */
	lastOutputAt: number;
};

/** How long the deploy has run, and a note when its log has gone quiet. */
function DeployProgress({ queuedAt, lastOutputAt }: DeployProgressProps) {
	const messages = useMessages().app.buildLog;
	const now = useNow();

	const elapsed = formatDuration(now - queuedAt * 1000);
	const quietForMs = now - lastOutputAt;
	const isQuiet = quietForMs >= quietAfterMs;

	return (
		<>
			<p>{messages.running(elapsed)}</p>
			{isQuiet && <p>{messages.quiet(formatDuration(quietForMs))}</p>}
		</>
	);
}

/** The current time, updated every second. */
function useNow(): number {
	const [now, setNow] = useState(() => Date.now());

	useEffect(() => {
		const timer = window.setInterval(() => setNow(Date.now()), 1000);
		return () => window.clearInterval(timer);
	}, []);

	return now;
}
