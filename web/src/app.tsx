import { useEffect, useState } from 'react';

import { HomePage } from './features/home/home-page';
import { LoginPage } from './features/login/login-page';
import { SetupPage } from './features/setup/setup-page';
import { type DashboardState, errorMessage, getState } from './lib/utils/api';
import { usePath } from './lib/utils/navigation';

type LoadedState = { state: DashboardState } | { error: string } | null;

/**
 * Shows the page for the current path and login state. The server enforces
 * every rule; this only decides which form to show.
 */
export function App() {
	const path = usePath();
	const [loaded, reload] = useDashboardState(path);

	if (path === '/setup') {
		return <SetupPage />;
	}

	if (loaded === null) {
		return <p>Loading…</p>;
	}

	if ('error' in loaded) {
		return <p role="alert">{loaded.error}</p>;
	}

	if (!loaded.state.isLoggedIn) {
		return <LoginPage isSetUp={loaded.state.isSetUp} onLogin={reload} />;
	}

	return <HomePage onLogout={reload} />;
}

/** Loads the login state, again whenever the path changes or reload is called. */
function useDashboardState(path: string): [LoadedState, () => void] {
	const [loaded, setLoaded] = useState<LoadedState>(null);
	const [reloadCount, setReloadCount] = useState(0);

	useEffect(() => {
		let isCurrent = true;
		setLoaded(null);

		getState()
			.then((state) => isCurrent && setLoaded({ state }))
			.catch((error: unknown) => isCurrent && setLoaded({ error: errorMessage(error) }));

		return () => {
			isCurrent = false;
		};
	}, [path, reloadCount]);

	const reload = () => setReloadCount((count) => count + 1);
	return [loaded, reload];
}
