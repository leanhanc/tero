import { useState } from 'react';

import { beginPasskeyLogin, errorMessage, finishPasskeyLogin } from '../../../lib/utils/api';
import { useMessages } from '../../../lib/utils/locale';
import { getPasskey, isCancelled, isPasskeySupported } from '../../../lib/utils/passkeys';

/**
 * Logs in with a passkey alone. The browser offers the passkeys it holds for
 * this domain, so no username is asked for. Browsers without passkey support
 * only see the password form.
 */
export function PasskeyLogin({ onLogin }: { onLogin: () => void }) {
	const messages = useMessages();
	const [error, setError] = useState('');
	const [isLoggingIn, setIsLoggingIn] = useState(false);

	if (!isPasskeySupported()) {
		return null;
	}

	const logIn = async () => {
		setIsLoggingIn(true);
		setError('');

		try {
			const { challengeId, options } = await beginPasskeyLogin();
			const credential = await getPasskey(options);
			await finishPasskeyLogin(challengeId, credential);
			onLogin();
		} catch (failure) {
			if (isCancelled(failure)) {
				return;
			}
			const isFromAuthenticator = failure instanceof DOMException;
			setError(isFromAuthenticator ? messages.login.passkey.failed : errorMessage(failure, messages.errors));
		} finally {
			setIsLoggingIn(false);
		}
	};

	return (
		<>
			<button type="button" onClick={logIn} disabled={isLoggingIn}>
				{messages.login.passkey.submit}
			</button>
			{error && <p role="alert">{error}</p>}
			<p>{messages.login.passkey.orPassword}</p>
		</>
	);
}
