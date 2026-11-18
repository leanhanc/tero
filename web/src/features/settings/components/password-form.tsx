import { type SubmitEvent, useState } from 'react';

import { AccountNameField } from '../../../components/account-name-field';
import { ApiError, changePassword, errorMessage } from '../../../lib/utils/api';
import { useMessages } from '../../../lib/utils/locale';
import { CodeInput } from '../../../components/code-input';
import { PasswordInput } from '../../../components/password-input';

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
		<section className="card" aria-labelledby="password-heading">
			<h2 className="h2" id="password-heading">
				{messages.settings.password.heading}
			</h2>
			<form className="stack" onSubmit={submit} aria-describedby={formError ? 'password-form-error' : undefined}>
				<AccountNameField />
				<div className="grid2">
					<div className="t-field">
						<label htmlFor="current-password">{messages.settings.password.currentLabel}</label>
						<PasswordInput
							id="current-password"
							name="current-password"
							autoComplete="current-password"
							value={currentPassword}
							onChange={setCurrentPassword}
						/>
					</div>
					<div className="t-field">
						<label htmlFor="password-code">{messages.settings.password.codeLabel}</label>
						<CodeInput id="password-code" value={code} onChange={setCode} describedBy="password-code-hint" />
						<span className="t-help" id="password-code-hint">
							{messages.common.codeHint}
						</span>
					</div>
				</div>
				<div className="t-field">
					<label htmlFor="new-password">{messages.settings.password.newLabel}</label>
					<PasswordInput
						id="new-password"
						name="new-password"
						autoComplete="new-password"
						minLength={8}
						value={newPassword}
						onChange={setNewPassword}
						describedBy={newPasswordDescription}
						isInvalid={hasNewPasswordError}
					/>
					<span className="t-help" id="new-password-hint">
						{messages.settings.password.newHint}
					</span>
					{hasNewPasswordError && (
						<span className="t-error" id="new-password-error" role="alert">
							{newPasswordError}
						</span>
					)}
				</div>
				<label className="t-check">
					<input type="checkbox" checked={isEndingOthers} onChange={(event) => setIsEndingOthers(event.target.checked)} />
					<b>{messages.settings.password.endOthers}</b>
				</label>
				{formError && (
					<p className="t-error" id="password-form-error" role="alert">
						{formError}
					</p>
				)}
				{isChanged && (
					<p className="t-note ok" role="status">
						{messages.settings.password.changed}
					</p>
				)}
				<div className="row">
					<button className="t-btn primary" type="submit" disabled={isSubmitting}>
						{messages.settings.password.submit}
					</button>
				</div>
			</form>
		</section>
	);
}
