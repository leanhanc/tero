import type { SubmitEvent } from 'react';

import { logout } from '../lib/utils/api';
import { useMessages } from '../lib/utils/locale';
import { InternalLink } from './internal-link';

/** The header of every signed-in page. */
export function PageHeader({ onLogout }: { onLogout: () => void }) {
	const messages = useMessages();

	const submitLogout = async (event: SubmitEvent<HTMLFormElement>) => {
		event.preventDefault();
		await logout().catch(() => undefined);
		onLogout();
	};

	return (
		<header>
			<p>
				<InternalLink to="/">{messages.header.home}</InternalLink>
			</p>
			<form onSubmit={submitLogout}>
				<button type="submit">{messages.header.logOut}</button>
			</form>
		</header>
	);
}
