import { useCallback, useEffect, useState } from 'react';

import { PageHeader } from '../../components/page-header';
import { type Account, errorMessage, getAccount, isConfirmationRequired } from '../../lib/utils/api';
import { useMessages } from '../../lib/utils/locale';
import { ConfirmForm } from './components/confirm-form';
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

/** The admin's password, passkeys and sessions. */
export function SettingsPage({ onLogout }: { onLogout: () => void }) {
	const messages = useMessages();
	const [loaded, reload] = useAccount(messages.errors);

	return (
		<>
			<PageHeader onLogout={onLogout} />
			<main>
				<h1>{messages.settings.heading}</h1>
				<PasswordForm onChange={reload} />
				<AccountSections loaded={loaded} onChange={reload} />
			</main>
		</>
	);
}

function AccountSections({ loaded, onChange }: { loaded: LoadedAccount; onChange: () => void }) {
	const messages = useMessages();
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
		return <p>{messages.common.loading}</p>;
	}

	if ('error' in loaded) {
		return <p role="alert">{loaded.error}</p>;
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
	return (
		<>
			{pendingAction && <ConfirmForm onConfirm={runPendingAction} onCancel={() => setPendingAction(null)} />}
			{error && <p role="alert">{error}</p>}
			{notice && <p role="status">{notice}</p>}
			<PasskeyList passkeys={account.passkeys} confirmation={confirmation} />
			<SessionList sessions={account.sessions} confirmation={confirmation} />
			<GitHubSection confirmation={confirmation} />
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
