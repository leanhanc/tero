import { useState } from 'react';

import { FingerprintIcon } from '../../../components/icons';
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
			<button className="t-btn primary wide" type="button" onClick={logIn} disabled={isLoggingIn}>
				<FingerprintIcon />
				{messages.login.passkey.submit}
			</button>
			{error && (
				<p className="t-error" role="alert">
					{error}
				</p>
			)}
			<p className="or">
				<span>{messages.login.passkey.orPassword}</span>
			</p>
		</>
	);
}
