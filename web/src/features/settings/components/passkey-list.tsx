import { type SubmitEvent, useState } from 'react';

import { errorMessage, type PasskeyInfo, removePasskey, renamePasskey } from '../../../lib/utils/api';
import { useFormatTime, useMessages } from '../../../lib/utils/locale';
import type { ConfirmThen } from '../settings-page';
import { AddPasskeyForm } from './add-passkey-form';

type PasskeyListProps = {
	passkeys: PasskeyInfo[];
	confirmThen: ConfirmThen;
};

/**
 * The admin's passkeys. Adding, renaming and removing one ask the admin to
 * confirm first when the session isn't confirmed; adding or removing one can
 * end every other session (v5.0.0-7.4.3).
 */
export function PasskeyList({ passkeys, confirmThen }: PasskeyListProps) {
	const messages = useMessages();
	const [isEndingOthers, setIsEndingOthers] = useState(true);
	const [status, setStatus] = useState('');
	const [error, setError] = useState('');

	const run = async (action: () => Promise<void>) => {
		setError('');
		setStatus('');
		try {
			await confirmThen(action);
		} catch (failure) {
			setError(errorMessage(failure, messages.errors));
		}
	};

	const remove = (passkey: PasskeyInfo) =>
		run(async () => {
			await removePasskey(passkey.id, isEndingOthers);
			setStatus(messages.settings.passkeys.removed(passkey.name));
		});

	return (
		<section aria-labelledby="passkeys-heading">
			<h2 id="passkeys-heading">{messages.settings.passkeys.heading}</h2>
			<p>{messages.settings.passkeys.intro}</p>
			{passkeys.length === 0 ? (
				<p>{messages.settings.passkeys.empty}</p>
			) : (
				<table>
					<caption>{messages.settings.passkeys.caption}</caption>
					<thead>
						<tr>
							<th scope="col">{messages.settings.passkeys.nameColumn}</th>
							<th scope="col">{messages.settings.passkeys.addedColumn}</th>
							<th scope="col">{messages.settings.passkeys.lastUsedColumn}</th>
							<th scope="col">{messages.settings.passkeys.actionsColumn}</th>
						</tr>
					</thead>
					<tbody>
						{passkeys.map((passkey) => (
							<PasskeyRow
								key={passkey.id}
								passkey={passkey}
								onRename={(name) => run(() => renamePasskey(passkey.id, name))}
								onRemove={() => remove(passkey)}
							/>
						))}
					</tbody>
				</table>
			)}
			{error && <p role="alert">{error}</p>}
			{status && <p role="status">{status}</p>}
			<input
				id="passkey-end-others"
				type="checkbox"
				checked={isEndingOthers}
				onChange={(event) => setIsEndingOthers(event.target.checked)}
			/>
			<label htmlFor="passkey-end-others">{messages.settings.passkeys.endOthers}</label>
			<AddPasskeyForm
				isEndingOthers={isEndingOthers}
				confirmThen={confirmThen}
				onAdd={(name) => setStatus(messages.settings.passkeys.added(name))}
			/>
		</section>
	);
}

type PasskeyRowProps = {
	passkey: PasskeyInfo;
	onRename: (name: string) => Promise<void>;
	onRemove: () => void;
};

function PasskeyRow({ passkey, onRename, onRemove }: PasskeyRowProps) {
	const messages = useMessages();
	const formatTime = useFormatTime();
	const [isRenaming, setIsRenaming] = useState(false);
	const lastUsed = passkey.lastUsedAt ? formatTime(new Date(passkey.lastUsedAt)) : messages.settings.passkeys.neverUsed;

	return (
		<tr>
			<th scope="row">
				{isRenaming ? (
					<RenameForm
						passkey={passkey}
						onSave={async (name) => {
							await onRename(name);
							setIsRenaming(false);
						}}
						onCancel={() => setIsRenaming(false)}
					/>
				) : (
					passkey.name
				)}
			</th>
			<td>{formatTime(new Date(passkey.createdAt))}</td>
			<td>{lastUsed}</td>
			<td>
				{!isRenaming && (
					<button type="button" onClick={() => setIsRenaming(true)}>
						{messages.settings.passkeys.rename}
					</button>
				)}
				<button type="button" onClick={onRemove}>
					{messages.settings.passkeys.remove(passkey.name)}
				</button>
			</td>
		</tr>
	);
}

type RenameFormProps = {
	passkey: PasskeyInfo;
	onSave: (name: string) => Promise<void>;
	onCancel: () => void;
};

function RenameForm({ passkey, onSave, onCancel }: RenameFormProps) {
	const messages = useMessages();
	const [name, setName] = useState(passkey.name);
	const inputID = `passkey-name-${passkey.id}`;

	const submit = async (event: SubmitEvent<HTMLFormElement>) => {
		event.preventDefault();
		await onSave(name);
	};

	return (
		<form onSubmit={submit}>
			<label htmlFor={inputID}>{messages.settings.passkeys.nameLabel}</label>
			<input
				id={inputID}
				type="text"
				required
				maxLength={64}
				value={name}
				onChange={(event) => setName(event.target.value)}
			/>
			<button type="submit">{messages.settings.passkeys.save}</button>
			<button type="button" onClick={onCancel}>
				{messages.settings.passkeys.cancel}
			</button>
		</form>
	);
}
