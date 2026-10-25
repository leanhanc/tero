import { type SubmitEvent, useState } from 'react';

import {
	ApiError,
	beginPasskeyRegistration,
	errorMessage,
	finishPasskeyRegistration,
} from '../../../lib/utils/api';
import { useMessages } from '../../../lib/utils/locale';
import { createPasskey, isCancelled, isPasskeySupported } from '../../../lib/utils/passkeys';

type AddPasskeyFormProps = {
	isEndingOthers: boolean;
	onAdd: (name: string) => void;
	onConfirmationOver: () => void;
};

/** Names a new passkey and has the browser create it for the key service to check. */
export function AddPasskeyForm({ isEndingOthers, onAdd, onConfirmationOver }: AddPasskeyFormProps) {
	const messages = useMessages();
	const [name, setName] = useState(messages.settings.passkeys.defaultName);
	const [error, setError] = useState('');
	const [isAdding, setIsAdding] = useState(false);

	if (!isPasskeySupported()) {
		return <p>{messages.settings.passkeys.unsupported}</p>;
	}

	const submit = async (event: SubmitEvent<HTMLFormElement>) => {
		event.preventDefault();
		setIsAdding(true);
		setError('');

		try {
			const { challengeId, options } = await beginPasskeyRegistration();
			const credential = await createPasskey(options);
			const added = await finishPasskeyRegistration(challengeId, name, credential, isEndingOthers);
			setName(messages.settings.passkeys.defaultName);
			onAdd(added.name);
		} catch (failure) {
			const isConfirmationOver = failure instanceof ApiError && failure.status === 403;
			if (isConfirmationOver) {
				onConfirmationOver();
				return;
			}
			if (isCancelled(failure)) {
				return;
			}
			const isFromAuthenticator = failure instanceof DOMException;
			setError(isFromAuthenticator ? messages.settings.passkeys.failed : errorMessage(failure, messages.errors));
		} finally {
			setIsAdding(false);
		}
	};

	return (
		<form onSubmit={submit} aria-describedby={error ? 'add-passkey-error' : undefined}>
			<label htmlFor="new-passkey-name">{messages.settings.passkeys.nameLabel}</label>
			<input
				id="new-passkey-name"
				type="text"
				required
				maxLength={64}
				value={name}
				onChange={(event) => setName(event.target.value)}
			/>
			{error && (
				<p id="add-passkey-error" role="alert">
					{error}
				</p>
			)}
			<button type="submit" disabled={isAdding}>
				{messages.settings.passkeys.add}
			</button>
		</form>
	);
}
