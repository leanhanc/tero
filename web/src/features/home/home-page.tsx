import type { SubmitEvent } from 'react';

import { logout } from '../../lib/utils/api';
import { SecurityEvents } from './components/security-events';

export function HomePage({ onLogout }: { onLogout: () => void }) {
	const submitLogout = async (event: SubmitEvent<HTMLFormElement>) => {
		event.preventDefault();
		await logout().catch(() => undefined);
		onLogout();
	};

	return (
		<>
			<header>
				<p>Tero</p>
				<form onSubmit={submitLogout}>
					<button type="submit">Log out</button>
				</form>
			</header>
			<main>
				<h1>Dashboard</h1>
				<SecurityEvents />
			</main>
		</>
	);
}
