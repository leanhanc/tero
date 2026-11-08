import { useState } from 'react';

import { errorMessage, getVariableReads, type VariableRead, type VariableScope } from '../../../lib/utils/api';
import { useFormatTime, useMessages } from '../../../lib/utils/locale';
import { type DraftVariable, isSealedRow, withSeal } from './variable-drafts';

type VariableRowProps = {
	appName: string;
	draft: DraftVariable;
	onChange: (next: DraftVariable) => void;
	onRemoveNew: () => void;
};

const scopes: VariableScope[] = ['both', 'runtime', 'build'];

export function VariableRow({ appName, draft, onChange, onRemoveNew }: VariableRowProps) {
	const messages = useMessages().app.variables;
	const isStored = draft.stored !== null;
	const isStoredSealed = isSealedRow(draft);
	const nameLabel = draft.name || messages.newVariable;

	return (
		<tr>
			<td>
				{isStored ? (
					draft.name
				) : (
					<input
						aria-label={messages.nameLabel}
						type="text"
						autoComplete="off"
						spellCheck={false}
						required
						value={draft.name}
						onChange={(event) => onChange({ ...draft, name: event.target.value })}
					/>
				)}
				{draft.isRemoved && <p>{messages.willBeRemoved}</p>}
			</td>
			<td>
				<ValueField draft={draft} label={messages.valueLabel(nameLabel)} onChange={onChange} />
			</td>
			<td>
				<select
					aria-label={messages.scopeLabel(nameLabel)}
					value={draft.scope}
					onChange={(event) => onChange({ ...draft, scope: event.target.value as VariableScope })}
				>
					{scopes.map((scope) => (
						<option key={scope} value={scope}>
							{messages.scopes[scope]}
						</option>
					))}
				</select>
				<p>{draft.isSealed ? messages.sealedScopeHint : messages.scopeHints[draft.scope]}</p>
			</td>
			<td>
				<SealCell draft={draft} isStoredSealed={isStoredSealed} label={messages.sealLabel(nameLabel)} onChange={onChange} />
				{isStoredSealed && <VariableReads appName={appName} name={draft.name} />}
			</td>
			<td>
				{isStored ? (
					<button type="button" onClick={() => onChange({ ...draft, isRemoved: !draft.isRemoved })}>
						{draft.isRemoved ? messages.keep : messages.remove}
					</button>
				) : (
					<button type="button" onClick={onRemoveNew}>
						{messages.remove}
					</button>
				)}
			</td>
		</tr>
	);
}

type ValueFieldProps = {
	draft: DraftVariable;
	label: string;
	onChange: (next: DraftVariable) => void;
};

/** A sealed variable's value is never shown: the field only takes a new one. */
function ValueField({ draft, label, onChange }: ValueFieldProps) {
	const messages = useMessages().app.variables;
	const isStoredSealed = isSealedRow(draft);
	const isUnreadable = draft.stored?.isUnreadable ?? false;

	return (
		<>
			<textarea
				aria-label={label}
				autoComplete="off"
				spellCheck={false}
				rows={1}
				placeholder={isStoredSealed ? messages.replacePlaceholder : undefined}
				value={draft.value}
				onChange={(event) => onChange({ ...draft, value: event.target.value })}
			/>
			{isUnreadable && <p role="alert">{messages.unreadable}</p>}
		</>
	);
}

type SealCellProps = {
	draft: DraftVariable;
	isStoredSealed: boolean;
	label: string;
	onChange: (next: DraftVariable) => void;
};

function SealCell({ draft, isStoredSealed, label, onChange }: SealCellProps) {
	const messages = useMessages().app.variables;
	const formatTime = useFormatTime();

	if (isStoredSealed) {
		const replacedAt = new Date((draft.stored?.replacedAt ?? 0) * 1000);
		return (
			<p>
				{messages.sealed} <time dateTime={replacedAt.toISOString()}>{messages.replacedAt(formatTime(replacedAt))}</time>
			</p>
		);
	}

	return (
		<>
			<input
				type="checkbox"
				aria-label={label}
				checked={draft.isSealed}
				onChange={(event) => onChange(withSeal(draft, event.target.checked))}
			/>
			{draft.isSealed && <p>{messages.sealWarning}</p>}
		</>
	);
}

/** When a sealed variable was handed to a deploy, from the security log. */
function VariableReads({ appName, name }: { appName: string; name: string }) {
	const allMessages = useMessages();
	const messages = allMessages.app.variables;
	const formatTime = useFormatTime();
	const [reads, setReads] = useState<VariableRead[] | null>(null);
	const [error, setError] = useState('');

	const load = async () => {
		setError('');
		try {
			setReads(await getVariableReads(appName, name));
		} catch (loadError) {
			setError(errorMessage(loadError, allMessages.errors));
		}
	};

	return (
		<>
			<button type="button" onClick={load}>
				{messages.showReads}
			</button>
			{error && <p role="alert">{error}</p>}
			{reads?.length === 0 && <p>{messages.noReads}</p>}
			{reads !== null && reads.length > 0 && (
				<ul aria-label={messages.readsLabel(name)}>
					{reads.map((read) => {
						const at = new Date(read.at * 1000);
						return (
							<li key={`${read.at}-${read.deployId}-${read.phase}`}>
								<time dateTime={at.toISOString()}>{formatTime(at)}</time> {messages.readPhases[read.phase](read.deployId)}
							</li>
						);
					})}
				</ul>
			)}
		</>
	);
}
