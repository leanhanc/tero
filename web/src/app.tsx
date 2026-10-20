import { useCallback, useEffect, useState } from 'react';

import { AppPage } from './features/app/app-page';
import { HomePage } from './features/home/home-page';
import { LoginPage } from './features/login/login-page';
import { NewAppPage } from './features/new-app/new-app-page';
import { SetupPage } from './features/setup/setup-page';
import { type DashboardState, errorMessage, getState } from './lib/utils/api';
import { usePath } from './lib/utils/navigation';

type LoadedState = { state: DashboardState } | { error: string } | null;

// The server ends a session after 30 minutes without a request. The signed-in
// page makes no requests after it loads, so the state is checked again just
// after that point, which swaps the page's data for the login form
// (OWASP ASVS v5.0.0-14.3.1).
const sessionIdleTimeoutMs = 30 * 60 * 1000;
const idleCheckMarginMs = 5 * 1000;

/**
 * Shows the page for the current path and login state. The server enforces
 * every rule; this only decides which form to show.
 */
export function App() {
	const path = usePath();
	const [loaded, reload] = useDashboardState(path);
	const isLoggedIn = loaded !== null && 'state' in loaded && loaded.state.isLoggedIn;
	useReloadWhenSessionIdles(isLoggedIn, reload);

	if (path === '/setup') {
		return <SetupPage />;
	}

	if (loaded === null) {
		return <p>Loading…</p>;
	}

	if ('error' in loaded) {
		return <p role="alert">{loaded.error}</p>;
	}

	if (!isLoggedIn) {
		return <LoginPage isSetUp={loaded.state.isSetUp} onLogin={reload} />;
	}

	return <SignedInPage path={path} onLogout={reload} />;
}

const appPathPattern = /^\/apps\/([a-z][a-z0-9-]*)$/;

/** Picks the signed-in page from the path. */
function SignedInPage({ path, onLogout }: { path: string; onLogout: () => void }) {
	if (path === '/apps/new') {
		return <NewAppPage onLogout={onLogout} />;
	}

	const appName = appPathPattern.exec(path)?.[1];
	if (appName) {
		return <AppPage key={appName} name={appName} onLogout={onLogout} />;
	}

	return <HomePage onLogout={onLogout} />;
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

	const reload = useCallback(() => setReloadCount((count) => count + 1), []);
	return [loaded, reload];
}

/** Checks the login state again once the server would have ended an idle session. */
function useReloadWhenSessionIdles(isLoggedIn: boolean, reload: () => void): void {
	useEffect(() => {
		if (!isLoggedIn) {
			return;
		}

		const timer = window.setTimeout(reload, sessionIdleTimeoutMs + idleCheckMarginMs);
		return () => window.clearTimeout(timer);
	}, [isLoggedIn, reload]);
}
