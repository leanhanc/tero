import { type SubmitEvent, useState } from 'react';

import { AccountNameField } from '../../../components/account-name-field';
import { ApiError, changePassword, errorMessage } from '../../../lib/utils/api';
import { useMessages } from '../../../lib/utils/locale';
import { CodeInput } from './code-input';

/**
 * Changes the password with the current one and a code in the same form, so
 * it never relies on an earlier confirmation (v5.0.0-6.2.3). A problem with
 * the new password belongs to its field; anything else to the form.
 */
export function PasswordForm({ onChange }: { onChange: () => void }) {
	const messages = useMessages();
	const [currentPassword, setCurrentPassword] = useState('');
	const [code, setCode] = useState('');
	const [newPassword, setNewPassword] = useState('');
	const [isEndingOthers, setIsEndingOthers] = useState(true);
	const [newPasswordError, setNewPasswordError] = useState('');
	const [formError, setFormError] = useState('');
	const [isChanged, setIsChanged] = useState(false);
	const [isSubmitting, setIsSubmitting] = useState(false);

	const submit = async (event: SubmitEvent<HTMLFormElement>) => {
		event.preventDefault();
		setIsSubmitting(true);
		setNewPasswordError('');
		setFormError('');
		setIsChanged(false);

		try {
			await changePassword({ currentPassword, code, newPassword, isEndingOthers });
			setCurrentPassword('');
			setNewPassword('');
			setIsChanged(true);
			onChange();
		} catch (error) {
			const isNewPasswordProblem = error instanceof ApiError && error.status === 422;
			if (isNewPasswordProblem) {
				setNewPasswordError(errorMessage(error, messages.errors));
			} else {
				setFormError(errorMessage(error, messages.errors));
			}
		} finally {
			setCode('');
			setIsSubmitting(false);
		}
	};

	const hasNewPasswordError = newPasswordError !== '';
	const newPasswordDescription = hasNewPasswordError ? 'new-password-hint new-password-error' : 'new-password-hint';

	return (
		<section aria-labelledby="password-heading">
			<h2 id="password-heading">{messages.settings.password.heading}</h2>
			<form onSubmit={submit} aria-describedby={formError ? 'password-form-error' : undefined}>
				<AccountNameField />
				<label htmlFor="current-password">{messages.settings.password.currentLabel}</label>
				<input
					id="current-password"
					name="current-password"
					type="password"
					autoComplete="current-password"
					required
					value={currentPassword}
					onChange={(event) => setCurrentPassword(event.target.value)}
				/>
				<label htmlFor="password-code">{messages.settings.password.codeLabel}</label>
				<CodeInput id="password-code" value={code} onChange={setCode} />
				<label htmlFor="new-password">{messages.settings.password.newLabel}</label>
				<input
					id="new-password"
					name="new-password"
					type="password"
					autoComplete="new-password"
					required
					minLength={8}
					value={newPassword}
					onChange={(event) => setNewPassword(event.target.value)}
					aria-describedby={newPasswordDescription}
					aria-invalid={hasNewPasswordError}
				/>
				<p id="new-password-hint">{messages.settings.password.newHint}</p>
				{hasNewPasswordError && (
					<p id="new-password-error" role="alert">
						{newPasswordError}
					</p>
				)}
				<input
					id="password-end-others"
					type="checkbox"
					checked={isEndingOthers}
					onChange={(event) => setIsEndingOthers(event.target.checked)}
				/>
				<label htmlFor="password-end-others">{messages.settings.password.endOthers}</label>
				{formError && (
					<p id="password-form-error" role="alert">
						{formError}
					</p>
				)}
				{isChanged && <p role="status">{messages.settings.password.changed}</p>}
				<button type="submit" disabled={isSubmitting}>
					{messages.settings.password.submit}
				</button>
			</form>
		</section>
	);
}
