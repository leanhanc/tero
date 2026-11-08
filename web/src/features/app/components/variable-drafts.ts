import type { Variable, VariableChange, VariableScope } from '../../../lib/utils/api';
import type { EnvEntry } from '../../../lib/utils/env-file';

/** A row of the variables form, as the admin has it so far. */
export type DraftVariable = {
	key: string;
	/** The variable as stored; null for a row added here. */
	stored: Variable | null;
	name: string;
	/** For a sealed variable, the new value being typed; empty keeps the stored one. */
	value: string;
	scope: VariableScope;
	isSealed: boolean;
	isRemoved: boolean;
};

let nextDraftKey = 0;

function draftKey(): string {
	nextDraftKey += 1;
	return `new-${nextDraftKey}`;
}

export function draftsFrom(variables: Variable[]): DraftVariable[] {
	return variables.map((variable) => ({
		key: variable.name,
		stored: variable,
		name: variable.name,
		value: variable.value ?? '',
		scope: variable.scope,
		isSealed: variable.isSealed,
		isRemoved: false,
	}));
}

/** A new variable reaches builds and the running app until the admin says otherwise. */
export function newDraft(entry?: EnvEntry): DraftVariable {
	return {
		key: draftKey(),
		stored: null,
		name: entry?.name ?? '',
		value: entry?.value ?? '',
		scope: 'both',
		isSealed: false,
		isRemoved: false,
	};
}

/** Sealing a variable keeps it out of builds unless the admin says otherwise. */
export function withSeal(draft: DraftVariable, isSealed: boolean): DraftVariable {
	return { ...draft, isSealed, scope: isSealed ? 'runtime' : draft.scope };
}

/** Whether a pasted line can't apply to this row, because its value is write-only. */
export function isSealedRow(draft: DraftVariable): boolean {
	return draft.stored?.isSealed ?? false;
}

/**
 * Adds and overwrites rows from a pasted file, and removes none. A sealed
 * variable is left as it is: it can only be replaced in its own row.
 */
export function addPastedEntries(drafts: DraftVariable[], entries: EnvEntry[]): DraftVariable[] {
	const next = [...drafts];
	for (const entry of entries) {
		const index = next.findIndex((draft) => draft.name === entry.name);
		const existing = next[index];
		if (existing === undefined) {
			next.push(newDraft(entry));
			continue;
		}
		if (!isSealedRow(existing)) {
			next[index] = { ...existing, value: entry.value, isRemoved: false };
		}
	}

	return next;
}

/** The changes a save sends: only what differs from what is stored. */
export function changesFrom(drafts: DraftVariable[]): VariableChange[] {
	return drafts.flatMap((draft) => {
		const change = changeFrom(draft);
		return change === null ? [] : [change];
	});
}

function changeFrom(draft: DraftVariable): VariableChange | null {
	const { stored, name, value, scope, isSealed, isRemoved } = draft;
	if (stored === null) {
		return isRemoved ? null : { name, value, scope, isSealed };
	}
	if (isRemoved) {
		return { name, isRemoved: true };
	}

	const hasNewValue = stored.isSealed ? value !== '' : value !== (stored.value ?? '');
	const hasOtherChange = scope !== stored.scope || isSealed !== stored.isSealed;
	if (!hasNewValue && !hasOtherChange) {
		return null;
	}

	return hasNewValue ? { name, value, scope, isSealed } : { name, scope, isSealed };
}
