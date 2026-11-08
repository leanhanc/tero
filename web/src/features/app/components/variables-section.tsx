import { type SubmitEvent, useCallback, useEffect, useState } from 'react';

import { ConfirmForm } from '../../../components/confirm-form';
import {
	type AppVariables,
	errorMessage,
	getVariables,
	isConfirmationRequired,
	saveVariables,
} from '../../../lib/utils/api';
import { useMessages } from '../../../lib/utils/locale';
import type { Messages } from '../../../locales/en';
import { EnvFilePaste } from './env-file-paste';
import { SourceApproval } from './source-approval';
import { addPastedEntries, changesFrom, type DraftVariable, draftsFrom, newDraft } from './variable-drafts';
import { VariableRow } from './variable-row';

type LoadedVariables = { variables: AppVariables } | { error: string } | null;

type VariablesSectionProps = {
	appName: string;
	onChange: () => void;
};

/** The app's variables, as one list, each with where it is used and an optional seal. */
export function VariablesSection({ appName, onChange }: VariablesSectionProps) {
	const allMessages = useMessages();
	const messages = allMessages.app.variables;
	const [loaded, reload] = useVariables(appName, allMessages.errors);

	return (
		<section aria-labelledby="variables-heading">
			<h2 id="variables-heading">{messages.heading}</h2>
			<p>{messages.intro}</p>
			{loaded === null && <p>{allMessages.common.loading}</p>}
			{loaded !== null && 'error' in loaded && <p role="alert">{loaded.error}</p>}
			{loaded !== null && 'variables' in loaded && (
				<VariablesEditor
					key={JSON.stringify(loaded.variables)}
					appName={appName}
					variables={loaded.variables}
					onSaved={() => {
						reload();
						onChange();
					}}
				/>
			)}
		</section>
	);
}

type VariablesEditorProps = {
	appName: string;
	variables: AppVariables;
	onSaved: () => void;
};

function VariablesEditor({ appName, variables, onSaved }: VariablesEditorProps) {
	const allMessages = useMessages();
	const messages = allMessages.app.variables;
	const [drafts, setDrafts] = useState<DraftVariable[]>(() => draftsFrom(variables.variables));
	const [error, setError] = useState('');
	const [isConfirming, setIsConfirming] = useState(false);
	const [isSaving, setIsSaving] = useState(false);

	const changeDraft = (key: string, next: DraftVariable) => {
		setDrafts((current) => current.map((draft) => (draft.key === key ? next : draft)));
	};

	const removeNewDraft = (key: string) => {
		setDrafts((current) => current.filter((draft) => draft.key !== key));
	};

	const save = async () => {
		setIsSaving(true);
		setError('');
		try {
			await saveVariables(appName, changesFrom(drafts));
			onSaved();
		} catch (saveError) {
			if (isConfirmationRequired(saveError)) {
				setIsConfirming(true);
				return;
			}
			setError(errorMessage(saveError, allMessages.errors));
		} finally {
			setIsSaving(false);
		}
	};

	const submit = (event: SubmitEvent<HTMLFormElement>) => {
		event.preventDefault();
		void save();
	};

	const confirmThenSave = () => {
		setIsConfirming(false);
		void save();
	};

	const hasChanges = changesFrom(drafts).length > 0;

	return (
		<>
			<SourceApproval appName={appName} variables={variables} onApprove={onSaved} />
			{isConfirming && <ConfirmForm onConfirm={confirmThenSave} onCancel={() => setIsConfirming(false)} />}
			<form onSubmit={submit}>
				<VariablesTable appName={appName} drafts={drafts} onChange={changeDraft} onRemoveNew={removeNewDraft} messages={messages} />
				<button type="button" onClick={() => setDrafts((current) => [...current, newDraft()])}>
					{messages.add}
				</button>
				<button type="submit" disabled={!hasChanges || isSaving || isConfirming} aria-describedby="variables-save-hint">
					{messages.save}
				</button>
				<p id="variables-save-hint">{messages.saveHint}</p>
				{error && <p role="alert">{error}</p>}
			</form>
			<EnvFilePaste onPaste={(entries) => setDrafts((current) => addPastedEntries(current, entries))} drafts={drafts} />
		</>
	);
}

type VariablesTableProps = {
	appName: string;
	drafts: DraftVariable[];
	onChange: (key: string, next: DraftVariable) => void;
	onRemoveNew: (key: string) => void;
	messages: Messages['app']['variables'];
};

function VariablesTable({ appName, drafts, onChange, onRemoveNew, messages }: VariablesTableProps) {
	if (drafts.length === 0) {
		return <p>{messages.empty}</p>;
	}

	return (
		<table>
			<caption>{messages.caption}</caption>
			<thead>
				<tr>
					<th scope="col">{messages.nameColumn}</th>
					<th scope="col">{messages.valueColumn}</th>
					<th scope="col">{messages.scopeColumn}</th>
					<th scope="col">{messages.sealedColumn}</th>
					<th scope="col">{messages.actionsColumn}</th>
				</tr>
			</thead>
			<tbody>
				{drafts.map((draft) => (
					<VariableRow
						key={draft.key}
						appName={appName}
						draft={draft}
						onChange={(next) => onChange(draft.key, next)}
						onRemoveNew={() => onRemoveNew(draft.key)}
					/>
				))}
			</tbody>
		</table>
	);
}

/** Loads the app's variables, again whenever reload is called. */
function useVariables(appName: string, errorMessages: Messages['errors']): [LoadedVariables, () => void] {
	const [loaded, setLoaded] = useState<LoadedVariables>(null);
	const [reloadCount, setReloadCount] = useState(0);

	useEffect(() => {
		let isCurrent = true;
		getVariables(appName)
			.then((variables) => isCurrent && setLoaded({ variables }))
			.catch((error: unknown) => isCurrent && setLoaded({ error: errorMessage(error, errorMessages) }));

		return () => {
			isCurrent = false;
		};
	}, [appName, reloadCount, errorMessages]);

	const reload = useCallback(() => setReloadCount((count) => count + 1), []);

	return [loaded, reload];
}
