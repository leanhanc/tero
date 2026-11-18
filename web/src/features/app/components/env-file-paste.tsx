import { useState } from 'react';

import { type EnvEntry, parseEnvFile } from '../../../lib/utils/env-file';
import { useMessages } from '../../../lib/utils/locale';
import { type DraftVariable, isSealedRow } from './variable-drafts';

type EnvFilePasteProps = {
	drafts: DraftVariable[];
	onPaste: (entries: EnvEntry[]) => void;
};

/** Fills the form from a pasted .env file. Nothing is stored until the admin saves. */
export function EnvFilePaste({ drafts, onPaste }: EnvFilePasteProps) {
	const messages = useMessages().app.variables.paste;
	const [text, setText] = useState('');
	const [report, setReport] = useState<string[]>([]);

	const addToForm = () => {
		const { entries, invalidLines } = parseEnvFile(text);
		const sealedNames = new Set(drafts.filter(isSealedRow).map((draft) => draft.name));
		const skipped = entries.filter((entry) => sealedNames.has(entry.name)).map((entry) => entry.name);

		onPaste(entries);
		setText('');
		setReport([
			...(skipped.length > 0 ? [messages.sealedSkipped(skipped.join(', '))] : []),
			...(invalidLines.length > 0 ? [messages.invalidLines(invalidLines.join(', '))] : []),
		]);
	};

	return (
		<section className="card" aria-labelledby="env-paste-heading">
			<h3 className="h3" id="env-paste-heading">
				{messages.heading}
			</h3>
			<div className="t-field">
				<label htmlFor="env-paste">{messages.label}</label>
				<textarea
					className="t-input code"
					id="env-paste"
					autoComplete="off"
					spellCheck={false}
					rows={6}
					value={text}
					onChange={(event) => setText(event.target.value)}
					aria-describedby="env-paste-hint"
				/>
				<span className="t-help" id="env-paste-hint">
					{messages.hint}
				</span>
			</div>
			<div className="row">
				<button className="t-btn secondary" type="button" onClick={addToForm} disabled={text.trim() === ''}>
					{messages.submit}
				</button>
			</div>
			{report.map((line) => (
				<p key={line} className="t-error" role="alert">
					{line}
				</p>
			))}
		</section>
	);
}
