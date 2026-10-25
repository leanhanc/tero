import { useEffect, useState } from 'react';

import { errorMessage, getAccount } from '../../lib/utils/api';
import { useMessages } from '../../lib/utils/locale';
import { dismissPasskeyOffer, isCancelled, registerPasskey } from '../../lib/utils/passkeys';

/**
 * Shown once after a login with the password and a code when the admin has
 * no passkey yet. The login just confirmed the session, so adding one needs
 * nothing more. Any problem skips the offer rather than stand in the way.
 */
export function PasskeyOfferPage({ onDone }: { onDone: () => void }) {
	const messages = useMessages();
	const [isReady, setIsReady] = useState(false);
	const [error, setError] = useState('');
	const [isAdding, setIsAdding] = useState(false);

	useEffect(() => {
		getAccount()
			.then((account) => (account.passkeys.length > 0 ? onDone() : setIsReady(true)))
			.catch(onDone);
	}, [onDone]);

	if (!isReady) {
		return <p>{messages.common.loading}</p>;
	}

	const add = async () => {
		setIsAdding(true);
		setError('');
		try {
			await registerPasskey(messages.settings.passkeys.defaultName, false);
			onDone();
		} catch (failure) {
			if (!isCancelled(failure)) {
				const isFromAuthenticator = failure instanceof DOMException;
				setError(isFromAuthenticator ? messages.settings.passkeys.failed : errorMessage(failure, messages.errors));
			}
		} finally {
			setIsAdding(false);
		}
	};

	const skip = () => {
		dismissPasskeyOffer();
		onDone();
	};

	return (
		<main>
			<h1>{messages.passkeyOffer.heading}</h1>
			<p>{messages.passkeyOffer.intro}</p>
			{error && <p role="alert">{error}</p>}
			<button type="button" onClick={add} disabled={isAdding}>
				{messages.settings.passkeys.add}
			</button>
			<button type="button" onClick={skip}>
				{messages.passkeyOffer.notNow}
			</button>
		</main>
	);
}
