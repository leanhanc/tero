import { type SubmitEvent, useState } from 'react';

import { AccountNameField } from '../../../components/account-name-field';
import { CodeInput } from '../../../components/code-input';
import { PasswordInput } from '../../../components/password-input';
import { errorMessage, login } from '../../../lib/utils/api';
import { useMessages } from '../../../lib/utils/locale';
import { requestPasskeyOffer } from '../../../lib/utils/passkeys';

/**
 * The server never says which factor was wrong, so the error belongs to the
 * whole form rather than to one field.
 */
export function LoginForm({ isMainWay, onLogin }: { isMainWay: boolean; onLogin: () => void }) {
	const messages = useMessages();
	const [password, setPassword] = useState('');
	const [code, setCode] = useState('');
	const [formError, setFormError] = useState('');
	const [isSubmitting, setIsSubmitting] = useState(false);
	// With a passkey button above, logging in with the password is the second way in.
	const submitStyle = isMainWay ? 'primary' : 'secondary';

	const submit = async (event: SubmitEvent<HTMLFormElement>) => {
		event.preventDefault();
		setIsSubmitting(true);
		setFormError('');

		try {
			await login(password, code);
			requestPasskeyOffer();
			onLogin();
		} catch (error) {
			setFormError(errorMessage(error, messages.errors));
			setCode('');
		} finally {
			setIsSubmitting(false);
		}
	};

	return (
		<form onSubmit={submit} aria-describedby={formError ? 'login-error' : undefined}>
			<AccountNameField />
			<div className="t-field">
				<label htmlFor="password">{messages.login.passwordLabel}</label>
				<PasswordInput id="password" name="password" autoComplete="current-password" value={password} onChange={setPassword} />
			</div>
			<div className="t-field">
				<label htmlFor="code">{messages.login.codeLabel}</label>
				<CodeInput id="code" value={code} onChange={setCode} describedBy="code-hint" />
				<span className="t-help" id="code-hint">
					{messages.common.codeHint}
				</span>
			</div>
			{formError && (
				<p className="t-error" id="login-error" role="alert">
					{formError}
				</p>
			)}
			<button className={`t-btn wide ${submitStyle}`} type="submit" disabled={isSubmitting}>
				{messages.login.submit}
			</button>
		</form>
	);
}
