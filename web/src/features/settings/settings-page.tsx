import { useCallback, useEffect, useState } from 'react';

import { PageHeader } from '../../components/page-header';
import { type Account, errorMessage, getAccount } from '../../lib/utils/api';
import { useMessages } from '../../lib/utils/locale';
import { ConfirmForm } from './components/confirm-form';
import { PasskeyList } from './components/passkey-list';
import { PasswordForm } from './components/password-form';
import { SessionList } from './components/session-list';

type LoadedAccount = { account: Account } | { error: string } | null;

/**
 * The admin's password, passkeys and sessions. Ending sessions and changing
 * passkeys need the session to be confirmed; until it is, the page asks for
 * the password and a code and leaves those actions out.
 */
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

	if (loaded === null) {
		return <p>{messages.common.loading}</p>;
	}

	if ('error' in loaded) {
		return <p role="alert">{loaded.error}</p>;
	}

	const { account } = loaded;
	return (
		<>
			{!account.isConfirmed && <ConfirmForm onConfirm={onChange} />}
			<PasskeyList passkeys={account.passkeys} isConfirmed={account.isConfirmed} onChange={onChange} />
			<SessionList sessions={account.sessions} isConfirmed={account.isConfirmed} onChange={onChange} />
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
