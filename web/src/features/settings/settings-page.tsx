import { useCallback, useEffect, useState } from 'react';

import { ConfirmForm } from '../../components/confirm-form';
import { AppShell } from '../../components/app-shell';
import { type Tab, TabPanel, Tabs, useTab } from '../../components/tabs';
import { type Account, errorMessage, getAccount, isConfirmationRequired } from '../../lib/utils/api';
import { useMessages } from '../../lib/utils/locale';
import { GitHubSection } from './components/github-section';
import { PasskeyList } from './components/passkey-list';
import { PasswordForm } from './components/password-form';
import { SessionList } from './components/session-list';

type LoadedAccount = { account: Account } | { error: string } | null;

/**
 * Runs an action that needs a confirmed session. When the session isn't
 * confirmed, the page asks for the password and a code first and then runs
 * the action; errors other than that refusal are the caller's to show.
 * Actions that open the browser's passkey prompt pass needsFreshClick:
 * browsers such as Safari only open it right after a click, so once
 * confirmed the admin presses the button again instead.
 */
export type ConfirmThen = (action: () => Promise<void>, options?: { needsFreshClick?: boolean }) => Promise<void>;

/** What the settings sections need to run actions that need a confirmed session. */
export type Confirmation = {
	confirmThen: ConfirmThen;
	/** True while the confirmation form is open; other actions wait for it. */
	isAwaiting: boolean;
};

type PendingAction = { run: () => Promise<void>; needsFreshClick: boolean };

const settingsTabs = ['github', 'access', 'sessions'] as const;
type SettingsTab = (typeof settingsTabs)[number];

/** The admin's password, passkeys and sessions. */
export function SettingsPage({ onLogout }: { onLogout: () => void }) {
	const messages = useMessages();
	const [loaded, reload] = useAccount(messages.errors);

	return (
		<AppShell section="settings" onLogout={onLogout}>
			<div className="ph">
				<h1>{messages.settings.heading}</h1>
			</div>
			<AccountSections loaded={loaded} onChange={reload} />
		</AppShell>
	);
}

function AccountSections({ loaded, onChange }: { loaded: LoadedAccount; onChange: () => void }) {
	const messages = useMessages();
	const [tab, setTab] = useTab(settingsTabs);
	const [pendingAction, setPendingAction] = useState<PendingAction | null>(null);
	const [error, setError] = useState('');
	const [notice, setNotice] = useState('');
	const isConfirmed = loaded !== null && 'account' in loaded && loaded.account.isConfirmed;

	const confirmThen: ConfirmThen = useCallback(
		async (action, options) => {
			const pending = { run: action, needsFreshClick: options?.needsFreshClick ?? false };
			setNotice('');
			if (!isConfirmed) {
				setPendingAction(pending);
				return;
			}
			try {
				await action();
				onChange();
			} catch (failure) {
				if (!isConfirmationRequired(failure)) {
					throw failure;
				}
				setPendingAction(pending);
			}
		},
		[isConfirmed, onChange],
	);

	if (loaded === null) {
		return <p className="muted">{messages.common.loading}</p>;
	}

	if ('error' in loaded) {
		return (
			<p className="t-error" role="alert">
				{loaded.error}
			</p>
		);
	}

	const runPendingAction = async () => {
		const pending = pendingAction;
		setPendingAction(null);
		setError('');
		onChange();
		if (!pending) {
			return;
		}
		if (pending.needsFreshClick) {
			setNotice(messages.settings.confirm.addPasskeyNow);
			return;
		}
		try {
			await pending.run();
		} catch (failure) {
			setError(errorMessage(failure, messages.errors));
		}
		onChange();
	};

	const { account } = loaded;
	const confirmation: Confirmation = { confirmThen, isAwaiting: pendingAction !== null };
	const tabs: Tab<SettingsTab>[] = settingsTabs.map((id) => ({ id, label: messages.settings.tabs[id] }));

	return (
		<>
			<Tabs label={messages.settings.heading} tabs={tabs} selected={tab} onSelect={setTab} />
			{pendingAction && <ConfirmForm onConfirm={runPendingAction} onCancel={() => setPendingAction(null)} />}
			{error && (
				<p className="t-error" role="alert">
					{error}
				</p>
			)}
			{notice && (
				<p className="t-note ok" role="status">
					{notice}
				</p>
			)}
			<TabPanel id={tab}>
				{tab === 'github' && <GitHubSection confirmation={confirmation} />}
				{tab === 'access' && (
					<>
						<PasskeyList passkeys={account.passkeys} confirmation={confirmation} />
						<PasswordForm onChange={onChange} />
					</>
				)}
				{tab === 'sessions' && <SessionList sessions={account.sessions} confirmation={confirmation} />}
			</TabPanel>
		</>
	);
}

/** Loads the account, again whenever reload is called. */
function useAccount(errorMessages: Parameters<typeof errorMessage>[1]): [LoadedAccount, () => void] {
	const [loaded, setLoaded] = useState<LoadedAccount>(null);
	const [reloadCount, setReloadCount] = useState(0);

	useEffect(() => {
		let isCurrent = true;

		getAccount()
			.then((account) => isCurrent && setLoaded({ account }))
			.catch((error: unknown) => isCurrent && setLoaded({ error: errorMessage(error, errorMessages) }));

		return () => {
			isCurrent = false;
		};
	}, [reloadCount, errorMessages]);

	const reload = useCallback(() => setReloadCount((count) => count + 1), []);
	return [loaded, reload];
}
