import { type SubmitEvent, useState } from 'react';

import { ApiError, errorMessage, type PasskeyInfo, removePasskey, renamePasskey } from '../../../lib/utils/api';
import { useFormatTime, useMessages } from '../../../lib/utils/locale';
import { AddPasskeyForm } from './add-passkey-form';

type PasskeyListProps = {
	passkeys: PasskeyInfo[];
	isConfirmed: boolean;
	onChange: () => void;
};

/**
 * The admin's passkeys. Adding, renaming and removing them need a confirmed
 * session; adding or removing one can end every other session
 * (v5.0.0-7.4.3).
 */
export function PasskeyList({ passkeys, isConfirmed, onChange }: PasskeyListProps) {
	const messages = useMessages();
	const [isEndingOthers, setIsEndingOthers] = useState(true);
	const [status, setStatus] = useState('');
	const [error, setError] = useState('');

	const run = async (action: () => Promise<void>, doneMessage: string) => {
		setError('');
		setStatus('');
		try {
			await action();
			setStatus(doneMessage);
			onChange();
		} catch (failure) {
			const isConfirmationOver = failure instanceof ApiError && failure.status === 403;
			if (isConfirmationOver) {
				onChange();
				return;
			}
			setError(errorMessage(failure, messages.errors));
		}
	};

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
							{isConfirmed && <th scope="col">{messages.settings.passkeys.actionsColumn}</th>}
						</tr>
					</thead>
					<tbody>
						{passkeys.map((passkey) => (
							<PasskeyRow
								key={passkey.id}
								passkey={passkey}
								isConfirmed={isConfirmed}
								onRename={(name) => run(() => renamePasskey(passkey.id, name), '')}
								onRemove={() =>
									run(() => removePasskey(passkey.id, isEndingOthers), messages.settings.passkeys.removed(passkey.name))
								}
							/>
						))}
					</tbody>
				</table>
			)}
			{error && <p role="alert">{error}</p>}
			{status && <p role="status">{status}</p>}
			{isConfirmed && (
				<>
					<input
						id="passkey-end-others"
						type="checkbox"
						checked={isEndingOthers}
						onChange={(event) => setIsEndingOthers(event.target.checked)}
					/>
					<label htmlFor="passkey-end-others">{messages.settings.passkeys.endOthers}</label>
					<AddPasskeyForm
						isEndingOthers={isEndingOthers}
						onAdd={(name) => {
							setError('');
							setStatus(messages.settings.passkeys.added(name));
							onChange();
						}}
						onConfirmationOver={onChange}
					/>
				</>
			)}
		</section>
	);
}

type PasskeyRowProps = {
	passkey: PasskeyInfo;
	isConfirmed: boolean;
	onRename: (name: string) => Promise<void>;
	onRemove: () => void;
};

function PasskeyRow({ passkey, isConfirmed, onRename, onRemove }: PasskeyRowProps) {
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
			{isConfirmed && (
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
			)}
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
