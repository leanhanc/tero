import type { ReactNode, SubmitEvent } from 'react';

import { logout } from '../lib/utils/api';
import { useMessages } from '../lib/utils/locale';
import { Brand } from './brand';
import { AppsIcon, LogOutIcon, SettingsIcon } from './icons';
import { InternalLink } from './internal-link';

export type Section = 'apps' | 'settings';

type AppShellProps = {
	section: Section;
	onLogout: () => void;
	children: ReactNode;
};

/** Every signed-in page: the side menu, and the page's own content next to it. */
export function AppShell({ section, onLogout, children }: AppShellProps) {
	const messages = useMessages();

	const submitLogout = async (event: SubmitEvent<HTMLFormElement>) => {
		event.preventDefault();
		await logout().catch(() => undefined);
		onLogout();
	};

	return (
		<div className="shell">
			<header className="side">
				<Brand />
				<nav className="t-nav" aria-label="Tero">
					<InternalLink to="/" isCurrent={section === 'apps'}>
						<AppsIcon />
						{messages.header.apps}
					</InternalLink>
					<InternalLink to="/settings" isCurrent={section === 'settings'}>
						<SettingsIcon />
						{messages.header.settings}
					</InternalLink>
				</nav>
				<form className="logout" onSubmit={submitLogout}>
					<button type="submit">
						<LogOutIcon />
						<span className="logout-label">{messages.header.logOut}</span>
					</button>
				</form>
			</header>
			<main className="content">{children}</main>
		</div>
	);
}
