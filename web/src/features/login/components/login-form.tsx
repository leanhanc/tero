import { type SubmitEvent, useState } from 'react';

import { AccountNameField } from '../../../components/account-name-field';
import { errorMessage, login } from '../../../lib/utils/api';

/**
 * The server never says which factor was wrong, so the error belongs to the
 * whole form rather than to one field.
 */
export function LoginForm({ onLogin }: { onLogin: () => void }) {
	const [password, setPassword] = useState('');
	const [code, setCode] = useState('');
	const [formError, setFormError] = useState('');
	const [isSubmitting, setIsSubmitting] = useState(false);

	const submit = async (event: SubmitEvent<HTMLFormElement>) => {
		event.preventDefault();
		setIsSubmitting(true);
		setFormError('');

		try {
			await login(password, code);
			onLogin();
		} catch (error) {
			setFormError(errorMessage(error));
			setCode('');
		} finally {
			setIsSubmitting(false);
		}
	};

	return (
		<form onSubmit={submit} aria-describedby={formError ? 'login-error' : undefined}>
			<AccountNameField />
			<label htmlFor="password">Password</label>
			<input
				id="password"
				name="password"
				type="password"
				autoComplete="current-password"
				required
				value={password}
				onChange={(event) => setPassword(event.target.value)}
			/>
			<label htmlFor="code">6-digit code from your authenticator app</label>
			<input
				id="code"
				name="code"
				type="text"
				inputMode="numeric"
				autoComplete="one-time-code"
				required
				pattern="[0-9]{6}"
				maxLength={6}
				value={code}
				onChange={(event) => setCode(event.target.value.trim())}
			/>
			{formError && (
				<p id="login-error" role="alert">
					{formError}
				</p>
			)}
			<button type="submit" disabled={isSubmitting}>
				Log in
			</button>
		</form>
	);
}
