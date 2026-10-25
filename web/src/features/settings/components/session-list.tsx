import { useState } from 'react';

import { ApiError, endOtherSessions, endSession, errorMessage, type SessionInfo } from '../../../lib/utils/api';
import { describeUserAgent } from '../../../lib/utils/browser-label';
import { useFormatTime, useMessages } from '../../../lib/utils/locale';

type SessionListProps = {
	sessions: SessionInfo[];
	isConfirmed: boolean;
	onChange: () => void;
};

/**
 * Every live session, and buttons to end one or all the others once the
 * session is confirmed (v5.0.0-7.5.2). This browser's own session ends with
 * logging out. A refusal because the confirmation
 * ran out reloads the page, which asks for it again.
 */
export function SessionList({ sessions, isConfirmed, onChange }: SessionListProps) {
	const messages = useMessages();
	const [error, setError] = useState('');
	const hasOtherSessions = sessions.some((session) => !session.isCurrent);

	const run = async (action: () => Promise<void>) => {
		setError('');
		try {
			await action();
			onChange();
		} catch (failure) {
			const isConfirmationOver = failure instanceof ApiError && failure.status === 403;
			if (isConfirmationOver) {
				onChange();
				return;
			}
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
						{isConfirmed && <th scope="col">{messages.settings.sessions.actionsColumn}</th>}
					</tr>
				</thead>
				<tbody>
					{sessions.map((session) => (
						<SessionRow
							key={session.id}
							session={session}
							isConfirmed={isConfirmed}
							onEnd={() => run(() => endSession(session.id))}
						/>
					))}
				</tbody>
			</table>
			{error && <p role="alert">{error}</p>}
			{isConfirmed && hasOtherSessions && (
				<button type="button" onClick={() => run(endOtherSessions)}>
					{messages.settings.sessions.endOthers}
				</button>
			)}
		</section>
	);
}

type SessionRowProps = {
	session: SessionInfo;
	isConfirmed: boolean;
	onEnd: () => void;
};

function SessionRow({ session, isConfirmed, onEnd }: SessionRowProps) {
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
			{isConfirmed && (
				<td>
					{!session.isCurrent && (
						<button type="button" onClick={onEnd} aria-label={`${messages.settings.sessions.end}: ${browser}`}>
							{messages.settings.sessions.end}
						</button>
					)}
				</td>
			)}
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
