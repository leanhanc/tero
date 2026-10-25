import { useCallback, useEffect, useState } from 'react';

import { PageHeader } from '../../components/page-header';
import { type Account, ApiError, errorMessage, getAccount } from '../../lib/utils/api';
import { useMessages } from '../../lib/utils/locale';
import { ConfirmForm } from './components/confirm-form';
import { PasskeyList } from './components/passkey-list';
import { PasswordForm } from './components/password-form';
import { SessionList } from './components/session-list';

type LoadedAccount = { account: Account } | { error: string } | null;

/**
 * Runs an action that needs a confirmed session. When the session isn't
 * confirmed, the page asks for the password and a code first and then runs
 * the action; errors other than that refusal are the caller's to show.
 */
export type ConfirmThen = (action: () => Promise<void>) => Promise<void>;

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
	const [pendingAction, setPendingAction] = useState<(() => Promise<void>) | null>(null);
	const [error, setError] = useState('');
	const isConfirmed = loaded !== null && 'account' in loaded && loaded.account.isConfirmed;

	const confirmThen: ConfirmThen = useCallback(
		async (action) => {
			if (!isConfirmed) {
				setPendingAction(() => action);
				return;
			}
			try {
				await action();
				onChange();
			} catch (failure) {
				if (!isConfirmationRefused(failure)) {
					throw failure;
				}
				setPendingAction(() => action);
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
		const action = pendingAction;
		setPendingAction(null);
		setError('');
		try {
			await action?.();
		} catch (failure) {
			setError(errorMessage(failure, messages.errors));
		}
		onChange();
	};

	const { account } = loaded;
	return (
		<>
			{pendingAction && <ConfirmForm onConfirm={runPendingAction} onCancel={() => setPendingAction(null)} />}
			{error && <p role="alert">{error}</p>}
			<PasskeyList passkeys={account.passkeys} confirmThen={confirmThen} />
			<SessionList sessions={account.sessions} confirmThen={confirmThen} />
		</>
	);
}

function isConfirmationRefused(failure: unknown): boolean {
	return failure instanceof ApiError && failure.status === 403;
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
