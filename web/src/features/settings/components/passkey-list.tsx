import { type SubmitEvent, useState } from 'react';

import { errorMessage, type PasskeyInfo, removePasskey, renamePasskey } from '../../../lib/utils/api';
import { useFormatTime, useMessages } from '../../../lib/utils/locale';
import { FingerprintIcon } from '../../../components/icons';
import type { Confirmation } from '../settings-page';
import { AddPasskeyForm } from './add-passkey-form';

type PasskeyListProps = {
	passkeys: PasskeyInfo[];
	confirmation: Confirmation;
};

/**
 * The admin's passkeys. Adding, renaming and removing one ask the admin to
 * confirm first when the session isn't confirmed; adding or removing one can
 * end every other session (v5.0.0-7.4.3).
 */
export function PasskeyList({ passkeys, confirmation }: PasskeyListProps) {
	const messages = useMessages();
	const [isEndingOthers, setIsEndingOthers] = useState(true);
	const [status, setStatus] = useState('');
	const [error, setError] = useState('');

	const run = async (action: () => Promise<void>) => {
		setError('');
		setStatus('');
		try {
			await confirmation.confirmThen(action);
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
		<section className="card" aria-labelledby="passkeys-heading">
			<h2 className="h2" id="passkeys-heading">
				{messages.settings.passkeys.heading}
			</h2>
			<p className="muted">{messages.settings.passkeys.intro}</p>
			{passkeys.length === 0 ? (
				<p className="muted">{messages.settings.passkeys.empty}</p>
			) : (
				<ul className="t-rows plain" aria-label={messages.settings.passkeys.caption}>
					{passkeys.map((passkey) => (
						<PasskeyRow
							key={passkey.id}
							passkey={passkey}
							isDisabled={confirmation.isAwaiting}
							onRename={(name) => run(() => renamePasskey(passkey.id, name))}
							onRemove={() => remove(passkey)}
						/>
					))}
				</ul>
			)}
			{error && (
				<p className="t-error" role="alert">
					{error}
				</p>
			)}
			{status && (
				<p className="t-note ok" role="status">
					{status}
				</p>
			)}
			<label className="t-check">
				<input type="checkbox" checked={isEndingOthers} onChange={(event) => setIsEndingOthers(event.target.checked)} />
				<b>{messages.settings.passkeys.endOthers}</b>
			</label>
			<AddPasskeyForm
				isEndingOthers={isEndingOthers}
				confirmation={confirmation}
				onAdd={(name) => setStatus(messages.settings.passkeys.added(name))}
			/>
		</section>
	);
}

type PasskeyRowProps = {
	passkey: PasskeyInfo;
	isDisabled: boolean;
	onRename: (name: string) => Promise<void>;
	onRemove: () => void;
};

function PasskeyRow({ passkey, isDisabled, onRename, onRemove }: PasskeyRowProps) {
	const messages = useMessages();
	const formatTime = useFormatTime();
	const [isRenaming, setIsRenaming] = useState(false);
	const lastUsed = passkey.lastUsedAt ? formatTime(new Date(passkey.lastUsedAt)) : messages.settings.passkeys.neverUsed;

	return (
		<li className="t-row">
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
				<span className="name">
					<FingerprintIcon />
					{passkey.name}
				</span>
			)}
			<div className="acts">
				{!isRenaming && (
					<button className="t-btn secondary sm" type="button" onClick={() => setIsRenaming(true)} disabled={isDisabled}>
						{messages.settings.passkeys.rename}
					</button>
				)}
				<button className="t-btn danger sm" type="button" onClick={onRemove} disabled={isDisabled}>
					{messages.settings.passkeys.remove(passkey.name)}
				</button>
			</div>
			<span className="meta">
				{messages.settings.passkeys.addedColumn}: {formatTime(new Date(passkey.createdAt))} · {messages.settings.passkeys.lastUsedColumn}:{' '}
				{lastUsed}
			</span>
		</li>
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
		<form className="t-field" onSubmit={submit}>
			<label htmlFor={inputID}>{messages.settings.passkeys.nameLabel}</label>
			<input
				className="t-input"
				id={inputID}
				type="text"
				required
				maxLength={64}
				value={name}
				onChange={(event) => setName(event.target.value)}
			/>
			<div className="row">
				<button className="t-btn secondary sm" type="submit">
					{messages.settings.passkeys.save}
				</button>
				<button className="t-btn quiet sm" type="button" onClick={onCancel}>
					{messages.settings.passkeys.cancel}
				</button>
			</div>
		</form>
	);
}
