import { type SubmitEvent, useEffect, useRef, useState } from 'react';

import { AccountNameField } from './account-name-field';
import { confirmIdentity, errorMessage } from '../lib/utils/api';
import { useMessages } from '../lib/utils/locale';
import { CodeInput } from './code-input';
import { PasswordInput } from './password-input';

type ConfirmFormProps = {
	onConfirm: () => void;
	onCancel: () => void;
};

/**
 * Asks for the password and a code so this session may do what needs a fresh
 * check, such as changing passkeys or an app's sealed variables. It appears when
 * the admin asks for one of those actions.
 */
export function ConfirmForm({ onConfirm, onCancel }: ConfirmFormProps) {
	const messages = useMessages();
	const [password, setPassword] = useState('');
	const [code, setCode] = useState('');
	const [formError, setFormError] = useState('');
	const [isSubmitting, setIsSubmitting] = useState(false);
	const passwordRef = useRef<HTMLInputElement>(null);

	// The form can open far from the button that asked for it, so it takes
	// focus and scrolls in.
	useEffect(() => {
		passwordRef.current?.focus();
	}, []);

	const submit = async (event: SubmitEvent<HTMLFormElement>) => {
		event.preventDefault();
		setIsSubmitting(true);
		setFormError('');

		try {
			await confirmIdentity(password, code);
			setPassword('');
			onConfirm();
		} catch (error) {
			setFormError(errorMessage(error, messages.errors));
		} finally {
			setCode('');
			setIsSubmitting(false);
		}
	};

	return (
		<section className="t-dialog" aria-labelledby="confirm-heading">
			<h2 className="h2" id="confirm-heading">
				{messages.settings.confirm.heading}
			</h2>
			<p id="confirm-intro">{messages.settings.confirm.intro}</p>
			<form className="stack" onSubmit={submit} aria-describedby={formError ? 'confirm-intro confirm-error' : 'confirm-intro'}>
				<AccountNameField />
				<div className="t-field">
					<label htmlFor="confirm-password">{messages.settings.confirm.passwordLabel}</label>
					<PasswordInput
						inputRef={passwordRef}
						id="confirm-password"
						name="password"
						autoComplete="current-password"
						value={password}
						onChange={setPassword}
					/>
				</div>
				<div className="t-field">
					<label htmlFor="confirm-code">{messages.settings.confirm.codeLabel}</label>
					<CodeInput id="confirm-code" value={code} onChange={setCode} />
				</div>
				{formError && (
					<p className="t-error" id="confirm-error" role="alert">
						{formError}
					</p>
				)}
				<div className="acts">
					<button className="t-btn quiet" type="button" onClick={onCancel}>
						{messages.settings.confirm.cancel}
					</button>
					<button className="t-btn primary" type="submit" disabled={isSubmitting}>
						{messages.settings.confirm.submit}
					</button>
				</div>
			</form>
		</section>
	);
}
