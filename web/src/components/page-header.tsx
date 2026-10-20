import type { SubmitEvent } from 'react';

import { logout } from '../lib/utils/api';
import { InternalLink } from './internal-link';

/** The header of every signed-in page. */
export function PageHeader({ onLogout }: { onLogout: () => void }) {
	const submitLogout = async (event: SubmitEvent<HTMLFormElement>) => {
		event.preventDefault();
		await logout().catch(() => undefined);
		onLogout();
	};

	return (
		<header>
			<p>
				<InternalLink to="/">Tero</InternalLink>
			</p>
			<form onSubmit={submitLogout}>
				<button type="submit">Log out</button>
			</form>
		</header>
	);
}
