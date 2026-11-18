import { useEffect, useState } from 'react';

import { AuthCard } from '../../components/auth-card';
import { FingerprintIcon } from '../../components/icons';
import { errorMessage, getAccount, isConfirmationRequired } from '../../lib/utils/api';
import { useMessages } from '../../lib/utils/locale';
import { dismissPasskeyOffer, isCancelled, registerPasskey } from '../../lib/utils/passkeys';
import type { Messages } from '../../locales/en';

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
		return (
			<AuthCard>
				<p className="muted">{messages.common.loading}</p>
			</AuthCard>
		);
	}

	const add = async () => {
		setIsAdding(true);
		setError('');
		try {
			await registerPasskey(messages.settings.passkeys.defaultName, false);
			onDone();
		} catch (failure) {
			setError(offerErrorMessage(failure, messages));
		} finally {
			setIsAdding(false);
		}
	};

	const skip = () => {
		dismissPasskeyOffer();
		onDone();
	};

	return (
		<AuthCard>
			<h1 className="ah">{messages.passkeyOffer.heading}</h1>
			<p className="muted">{messages.passkeyOffer.intro}</p>
			<div className="finger">
				<FingerprintIcon />
			</div>
			{error && (
				<p className="t-error" role="alert">
					{error}
				</p>
			)}
			<button className="t-btn primary wide" type="button" onClick={add} disabled={isAdding}>
				{messages.settings.passkeys.add}
			</button>
			<button className="t-btn quiet wide" type="button" onClick={skip}>
				{messages.passkeyOffer.notNow}
			</button>
		</AuthCard>
	);
}

/**
 * The message for a failed offer, or none when the admin dismissed the
 * browser's prompt. The login's confirmation lasts 5 minutes, so a page left
 * open longer can only point to Settings, which asks for it again.
 */
function offerErrorMessage(failure: unknown, messages: Messages): string {
	if (isCancelled(failure)) {
		return '';
	}
	if (isConfirmationRequired(failure)) {
		return messages.passkeyOffer.expired;
	}
	if (failure instanceof DOMException) {
		return messages.settings.passkeys.failed;
	}

	return errorMessage(failure, messages.errors);
}
