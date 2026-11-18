import { useState } from 'react';

import { DesktopIcon } from '../../../components/icons';
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
		<section className="stack" aria-labelledby="sessions-heading">
			<h2 className="h2" id="sessions-heading">
				{messages.settings.sessions.heading}
			</h2>
			<p className="muted">{messages.settings.sessions.caption}</p>
			<ul className="t-rows plain" aria-label={messages.settings.sessions.caption}>
				{sessions.map((session) => (
					<SessionRow
						key={session.id}
						session={session}
						isDisabled={confirmation.isAwaiting}
						onEnd={() => run(() => endSession(session.id))}
					/>
				))}
			</ul>
			{error && (
				<p className="t-error" role="alert">
					{error}
				</p>
			)}
			{hasOtherSessions && (
				<div className="row">
					<button className="t-btn secondary" type="button" onClick={() => run(endOtherSessions)} disabled={confirmation.isAwaiting}>
						{messages.settings.sessions.endOthers}
					</button>
				</div>
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
		<li className="t-row">
			<span className="name">
				<DesktopIcon />
				{browser}
			</span>
			{session.isCurrent ? (
				<span className="t-chip">{messages.settings.sessions.current}</span>
			) : (
				<div className="acts">
					<button
						className="t-btn secondary sm"
						type="button"
						onClick={onEnd}
						disabled={isDisabled}
						aria-label={`${messages.settings.sessions.end}: ${browser}`}
					>
						{messages.settings.sessions.end}
					</button>
				</div>
			)}
			<span className="meta">
				{messages.settings.sessions.methods[session.method]} · {messages.settings.sessions.startedColumn}{' '}
				{formatTime(new Date(session.createdAt))} · {messages.settings.sessions.lastUsedColumn} {formatTime(new Date(session.lastSeenAt))} ·{' '}
				<span className="mono-s">{ip}</span>
			</span>
		</li>
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
