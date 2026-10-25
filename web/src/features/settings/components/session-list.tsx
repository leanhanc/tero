import { useState } from 'react';

import { endOtherSessions, endSession, errorMessage, type SessionInfo } from '../../../lib/utils/api';
import { describeUserAgent } from '../../../lib/utils/browser-label';
import { useFormatTime, useMessages } from '../../../lib/utils/locale';
import type { Confirmation } from '../settings-page';

type SessionListProps = {
	sessions: SessionInfo[];
	confirmation: Confirmation;
};

/**
 * Every live session, and buttons to end one or all the others, which ask
 * the admin to confirm first when the session isn't confirmed
 * (v5.0.0-7.5.2). This browser's own session ends with logging out.
 */
export function SessionList({ sessions, confirmation }: SessionListProps) {
	const messages = useMessages();
	const [error, setError] = useState('');
	const hasOtherSessions = sessions.some((session) => !session.isCurrent);

	const run = async (action: () => Promise<void>) => {
		setError('');
		try {
			await confirmation.confirmThen(action);
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
						<SessionRow
							key={session.id}
							session={session}
							isDisabled={confirmation.isAwaiting}
							onEnd={() => run(() => endSession(session.id))}
						/>
					))}
				</tbody>
			</table>
			{error && <p role="alert">{error}</p>}
			{hasOtherSessions && (
				<button type="button" onClick={() => run(endOtherSessions)} disabled={confirmation.isAwaiting}>
					{messages.settings.sessions.endOthers}
				</button>
			)}
		</section>
	);
}

type SessionRowProps = {
	session: SessionInfo;
	isDisabled: boolean;
	onEnd: () => void;
};

function SessionRow({ session, isDisabled, onEnd }: SessionRowProps) {
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
					<button
						type="button"
						onClick={onEnd}
						disabled={isDisabled}
						aria-label={`${messages.settings.sessions.end}: ${browser}`}
					>
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
