import { type SubmitEvent, useState } from 'react';

import { errorMessage } from '../../../lib/utils/api';
import { useMessages } from '../../../lib/utils/locale';
import { isCancelled, isPasskeySupported, registerPasskey } from '../../../lib/utils/passkeys';
import type { Confirmation } from '../settings-page';

type AddPasskeyFormProps = {
	isEndingOthers: boolean;
	confirmation: Confirmation;
	onAdd: (name: string) => void;
};

/** Names a new passkey and has the browser create it for the key service to check. */
export function AddPasskeyForm({ isEndingOthers, confirmation, onAdd }: AddPasskeyFormProps) {
	const messages = useMessages();
	const [name, setName] = useState(messages.settings.passkeys.defaultName);
	const [error, setError] = useState('');
	const [isAdding, setIsAdding] = useState(false);

	if (!isPasskeySupported()) {
		return <p>{messages.settings.passkeys.unsupported}</p>;
	}

	const add = async () => {
		const added = await registerPasskey(name, isEndingOthers);
		setName(messages.settings.passkeys.defaultName);
		onAdd(added.name);
	};

	const submit = async (event: SubmitEvent<HTMLFormElement>) => {
		event.preventDefault();
		setIsAdding(true);
		setError('');

		try {
			await confirmation.confirmThen(add, { needsFreshClick: true });
		} catch (failure) {
			if (!isCancelled(failure)) {
				const isFromAuthenticator = failure instanceof DOMException;
				setError(isFromAuthenticator ? messages.settings.passkeys.failed : errorMessage(failure, messages.errors));
			}
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
			<button type="submit" disabled={isAdding || confirmation.isAwaiting}>
				{messages.settings.passkeys.add}
			</button>
		</form>
	);
}
