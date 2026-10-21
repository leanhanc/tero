import { useEffect, useState } from 'react';

import { type Deploy, deployLogURL } from '../../../lib/utils/api';
import { useMessages } from '../../../lib/utils/locale';

type LogStatus = 'streaming' | 'finished' | 'unavailable';

/**
 * Streams a deploy's build log from the server and shows it as plain text.
 * React escapes it, so build output can never become markup.
 */
export function BuildLog({ deploy }: { deploy: Deploy }) {
	const messages = useMessages().app.buildLog;
	const [log, setLog] = useState('');
	const [status, setStatus] = useState<LogStatus>('streaming');

	useEffect(() => {
		const source = new EventSource(deployLogURL(deploy.id));

		source.addEventListener('log', (event) => {
			const chunk = JSON.parse((event as MessageEvent<string>).data) as string;
			setLog((current) => current + chunk);
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

	return (
		<section aria-labelledby="build-log-heading">
			<h2 id="build-log-heading">{messages.heading(deploy.id)}</h2>
			{status === 'streaming' && <p aria-live="polite">{messages.streaming}</p>}
			{status === 'unavailable' && <p role="alert">{messages.unavailable}</p>}
			<pre>{log || messages.empty}</pre>
		</section>
	);
}
