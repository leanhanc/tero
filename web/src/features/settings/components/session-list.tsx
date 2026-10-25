import { useState } from 'react';

import { endOtherSessions, endSession, errorMessage, type SessionInfo } from '../../../lib/utils/api';
import { describeUserAgent } from '../../../lib/utils/browser-label';
import { useFormatTime, useMessages } from '../../../lib/utils/locale';
import type { ConfirmThen } from '../settings-page';

type SessionListProps = {
	sessions: SessionInfo[];
	confirmThen: ConfirmThen;
};

/**
 * Every live session, and buttons to end one or all the others, which ask
 * the admin to confirm first when the session isn't confirmed
 * (v5.0.0-7.5.2). This browser's own session ends with logging out.
 */
export function SessionList({ sessions, confirmThen }: SessionListProps) {
	const messages = useMessages();
	const [error, setError] = useState('');
	const hasOtherSessions = sessions.some((session) => !session.isCurrent);

	const run = async (action: () => Promise<void>) => {
		setError('');
		try {
			await confirmThen(action);
		} catch (failure) {
			setError(errorMessage(failure, messages.errors));
		}
	};

	return (
		<section aria-labelledby="sessions-heading">
			<h2 id="sessions-heading">{messages.settings.sessions.heading}</h2>
			<table>
				<caption>{messages.settings.sessions.caption}</caption>
				<thead>
					<tr>
						<th scope="col">{messages.settings.sessions.browserColumn}</th>
						<th scope="col">{messages.settings.sessions.methodColumn}</th>
						<th scope="col">{messages.settings.sessions.startedColumn}</th>
						<th scope="col">{messages.settings.sessions.lastUsedColumn}</th>
						<th scope="col">{messages.settings.sessions.ipColumn}</th>
						<th scope="col">{messages.settings.sessions.actionsColumn}</th>
					</tr>
				</thead>
				<tbody>
					{sessions.map((session) => (
						<SessionRow key={session.id} session={session} onEnd={() => run(() => endSession(session.id))} />
					))}
				</tbody>
			</table>
			{error && <p role="alert">{error}</p>}
			{hasOtherSessions && (
				<button type="button" onClick={() => run(endOtherSessions)}>
					{messages.settings.sessions.endOthers}
				</button>
			)}
		</section>
	);
}

function SessionRow({ session, onEnd }: { session: SessionInfo; onEnd: () => void }) {
	const messages = useMessages();
	const formatTime = useFormatTime();
	const browser = useBrowserName(session.userAgent);
	const hasMovedIP = session.lastIP !== session.loginIP;
	const ip = hasMovedIP ? messages.settings.sessions.ipChanged(session.loginIP, session.lastIP) : session.loginIP;

	return (
		<tr>
			<th scope="row">
				{browser}
				{session.isCurrent && <> ({messages.settings.sessions.current})</>}
			</th>
			<td>{messages.settings.sessions.methods[session.method]}</td>
			<td>{formatTime(new Date(session.createdAt))}</td>
			<td>{formatTime(new Date(session.lastSeenAt))}</td>
			<td>{ip}</td>
			<td>
				{!session.isCurrent && (
					<button type="button" onClick={onEnd} aria-label={`${messages.settings.sessions.end}: ${browser}`}>
						{messages.settings.sessions.end}
					</button>
				)}
			</td>
		</tr>
	);
}

/** "Firefox on macOS", or the user agent itself when it names no common browser. */
function useBrowserName(userAgent: string): string {
	const messages = useMessages();
	const described = describeUserAgent(userAgent);

	if (described) {
		return messages.settings.sessions.browserOn(described.browser, described.system);
	}

	return userAgent || messages.settings.sessions.unknownBrowser;
}
