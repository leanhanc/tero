import { useCallback, useEffect, useState } from 'react';

import { PageHeader } from '../../components/page-header';
import { type AppDetail, errorMessage, getApp } from '../../lib/utils/api';
import { isInProgress } from '../../lib/utils/deploy-states';
import { useMessages } from '../../lib/utils/locale';
import type { Messages } from '../../locales/en';
import { AppSummary } from './components/app-summary';
import { BuildLog } from './components/build-log';
import { DeployList } from './components/deploy-list';
import { DisplayNameSetting } from './components/display-name-setting';
import { RedeployButton } from './components/redeploy-button';
import { RunTogetherSetting } from './components/run-together-setting';

// How often the page refreshes while a deploy is on its way.
const refreshIntervalMs = 2000;

type LoadedApp = { app: AppDetail } | { error: string } | null;

export function AppPage({ name, onLogout }: { name: string; onLogout: () => void }) {
	const messages = useMessages();
	const [loaded, reload] = useApp(name, messages.errors);
	const [selectedDeployID, setSelectedDeployID] = useState<number | null>(null);
	const heading = loaded !== null && 'app' in loaded ? loaded.app.displayName : name;

	return (
		<>
			<PageHeader onLogout={onLogout} />
			<main>
				<h1>{heading}</h1>
				<AppPageContent
					loaded={loaded}
					selectedDeployID={selectedDeployID}
					onSelectDeploy={setSelectedDeployID}
					onChange={reload}
				/>
			</main>
		</>
	);
}

type AppPageContentProps = {
	loaded: LoadedApp;
	selectedDeployID: number | null;
	onSelectDeploy: (deployID: number) => void;
	onChange: () => void;
};

function AppPageContent({ loaded, selectedDeployID, onSelectDeploy, onChange }: AppPageContentProps) {
	const messages = useMessages();

	if (loaded === null) {
		return <p>{messages.common.loading}</p>;
	}

	if ('error' in loaded) {
		return <p role="alert">{loaded.error}</p>;
	}

	const { app } = loaded;
	const latestDeploy = app.deploys[0];
	const hasDeployInProgress = latestDeploy !== undefined && isInProgress(latestDeploy.state);
	const shownDeploy = app.deploys.find((deploy) => deploy.id === selectedDeployID) ?? latestDeploy;

	return (
		<>
			<AppSummary app={app} />
			<RedeployButton appName={app.name} hasDeployInProgress={hasDeployInProgress} onRedeploy={onChange} />
			<DisplayNameSetting key={app.displayName} appName={app.name} displayName={app.displayName} onChange={onChange} />
			<RunTogetherSetting appName={app.name} runTogether={app.runTogether} onChange={onChange} />
			<DeployList deploys={app.deploys} shownDeployID={shownDeploy?.id} onSelect={onSelectDeploy} />
			{shownDeploy && <BuildLog key={shownDeploy.id} deploy={shownDeploy} />}
		</>
	);
}

/** Loads the app, and keeps reloading it while a deploy is in progress. */
function useApp(name: string, errorMessages: Messages['errors']): [LoadedApp, () => void] {
	const [loaded, setLoaded] = useState<LoadedApp>(null);
	const [reloadCount, setReloadCount] = useState(0);

	useEffect(() => {
		let isCurrent = true;
		getApp(name)
			.then((app) => isCurrent && setLoaded({ app }))
			.catch((error: unknown) => isCurrent && setLoaded({ error: errorMessage(error, errorMessages) }));

		return () => {
			isCurrent = false;
		};
	}, [name, reloadCount, errorMessages]);

	const reload = useCallback(() => setReloadCount((count) => count + 1), []);

	const latestState = loaded !== null && 'app' in loaded ? (loaded.app.deploys[0]?.state ?? '') : '';
	const shouldRefresh = isInProgress(latestState);
	useEffect(() => {
		if (!shouldRefresh) {
			return;
		}

		const timer = window.setTimeout(reload, refreshIntervalMs);
		return () => window.clearTimeout(timer);
	}, [shouldRefresh, loaded, reload]);

	return [loaded, reload];
}
