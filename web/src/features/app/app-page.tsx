import { useCallback, useEffect, useState } from 'react';

import { AppShell } from '../../components/app-shell';
import { type Tab, TabPanel, Tabs, useTab } from '../../components/tabs';
import { type AppDetail, errorMessage, getApp } from '../../lib/utils/api';
import { isInProgress } from '../../lib/utils/deploy-states';
import { useMessages } from '../../lib/utils/locale';
import type { Messages } from '../../locales/en';
import { AppHeader } from './components/app-header';
import { AppSummary } from './components/app-summary';
import { BuildLog } from './components/build-log';
import { DeployList } from './components/deploy-list';
import { DeployOnPushSetting } from './components/deploy-on-push-setting';
import { DisplayNameSetting } from './components/display-name-setting';
import { PendingChanges } from './components/pending-changes';
import { RunTogetherSetting } from './components/run-together-setting';
import { VariablesSection } from './components/variables-section';

// How often the page refreshes while a deploy is on its way.
const refreshIntervalMs = 2000;

const appTabs = ['summary', 'deploys', 'variables', 'settings'] as const;
type AppTab = (typeof appTabs)[number];

type LoadedApp = { app: AppDetail } | { error: string } | null;

export function AppPage({ name, onLogout }: { name: string; onLogout: () => void }) {
	const messages = useMessages();
	const [loaded, reload] = useApp(name, messages.errors);

	return (
		<AppShell section="apps" onLogout={onLogout}>
			<AppPageContent name={name} loaded={loaded} onChange={reload} />
		</AppShell>
	);
}

type AppPageContentProps = {
	name: string;
	loaded: LoadedApp;
	onChange: () => void;
};

function AppPageContent({ name, loaded, onChange }: AppPageContentProps) {
	const messages = useMessages();

	if (loaded === null) {
		return (
			<>
				<h1>{name}</h1>
				<p className="muted">{messages.common.loading}</p>
			</>
		);
	}

	if ('error' in loaded) {
		return (
			<>
				<h1>{name}</h1>
				<p className="t-error" role="alert">
					{loaded.error}
				</p>
			</>
		);
	}

	return <AppDetails app={loaded.app} onChange={onChange} />;
}

/** An app in three parts: what is live, its deploys, its variables and its settings. */
function AppDetails({ app, onChange }: { app: AppDetail; onChange: () => void }) {
	const messages = useMessages().app;
	const [tab, setTab] = useTab(appTabs);
	const [selectedDeployID, setSelectedDeployID] = useState<number | null>(null);
	const tabs: Tab<AppTab>[] = appTabs.map((id) => ({ id, label: messages.tabs[id] }));

	// A queued deploy has no log yet, so the page opens on the newest one that
	// has started.
	const latestStartedDeploy = app.deploys.find((deploy) => deploy.state !== 'queued') ?? app.deploys[0];
	const shownDeploy = app.deploys.find((deploy) => deploy.id === selectedDeployID) ?? latestStartedDeploy;

	return (
		<>
			<AppHeader app={app} onRedeploy={onChange} />
			<AppProblems app={app} />
			<PendingChanges app={app} onRedeploy={onChange} />
			<Tabs label={app.displayName} tabs={tabs} selected={tab} onSelect={setTab} />
			<TabPanel id={tab}>
				{tab === 'summary' && (
					<AppSummary app={app} latestDeploy={latestStartedDeploy} onShowDeploys={() => setTab('deploys')} />
				)}
				{tab === 'deploys' && (
					<>
						<DeployList deploys={app.deploys} shownDeployID={shownDeploy?.id} onSelect={setSelectedDeployID} />
						{shownDeploy && <BuildLog key={shownDeploy.id} deploy={shownDeploy} />}
					</>
				)}
				{tab === 'variables' && <VariablesSection appName={app.name} onChange={onChange} />}
				{tab === 'settings' && <AppSettings app={app} onChange={onChange} />}
			</TabPanel>
		</>
	);
}

/** Problems Tero can't fix alone, shown above every tab. */
function AppProblems({ app }: { app: AppDetail }) {
	const messages = useMessages().app.summary;
	const problems = [app.pollError && messages.pollStopped(app.pollError), app.startProblem].filter(Boolean);

	return problems.map((problem) => (
		<div key={problem} className="t-note err" role="alert">
			<span className="t-state err" aria-hidden="true" />
			<div>
				<p>{problem}</p>
			</div>
		</div>
	));
}

function AppSettings({ app, onChange }: { app: AppDetail; onChange: () => void }) {
	const messages = useMessages().app.settingsCards;

	return (
		<>
			<section className="card" aria-labelledby="name-card-heading">
				<h2 className="h2" id="name-card-heading">
					{messages.name}
				</h2>
				<DisplayNameSetting key={app.displayName} appName={app.name} displayName={app.displayName} onChange={onChange} />
			</section>
			<section className="card" aria-labelledby="deploys-card-heading">
				<h2 className="h2" id="deploys-card-heading">
					{messages.deploys}
				</h2>
				<DeployOnPushSetting appName={app.name} deployOnPush={app.deployOnPush} onChange={onChange} />
				<RunTogetherSetting appName={app.name} runTogether={app.runTogether} onChange={onChange} />
			</section>
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

	const deploys = loaded !== null && 'app' in loaded ? loaded.app.deploys : [];
	const shouldRefresh = deploys.some((deploy) => isInProgress(deploy.state));
	useEffect(() => {
		if (!shouldRefresh) {
			return;
		}

		const timer = window.setTimeout(reload, refreshIntervalMs);
		return () => window.clearTimeout(timer);
	}, [shouldRefresh, loaded, reload]);

	return [loaded, reload];
}
