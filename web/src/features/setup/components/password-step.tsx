import { type SubmitEvent, useState } from 'react';

import { AccountNameField } from '../../../components/account-name-field';
import { ApiError, beginSetup, type Enrolment, errorMessage } from '../../../lib/utils/api';
import { useMessages } from '../../../lib/utils/locale';

type PasswordStepProps = {
	token: string;
	onEnrolment: (enrolment: Enrolment) => void;
	onLinkError: (message: string) => void;
};

export function PasswordStep({ token, onEnrolment, onLinkError }: PasswordStepProps) {
	const messages = useMessages();
	const [password, setPassword] = useState('');
	const [passwordError, setPasswordError] = useState('');
	const [isSubmitting, setIsSubmitting] = useState(false);

	const submit = async (event: SubmitEvent<HTMLFormElement>) => {
		event.preventDefault();
		setIsSubmitting(true);
		setPasswordError('');

		try {
			onEnrolment(await beginSetup(token, password));
		} catch (error) {
			const isPasswordProblem = error instanceof ApiError && error.status === 422;
			if (isPasswordProblem) {
				setPasswordError(errorMessage(error, messages.errors));
			} else {
				onLinkError(errorMessage(error, messages.errors));
			}
		} finally {
			setIsSubmitting(false);
		}
	};

	const hasError = passwordError !== '';
	const describedBy = hasError ? 'password-hint password-error' : 'password-hint';

	return (
		<form onSubmit={submit}>
			<h2>{messages.setup.password.heading}</h2>
			<AccountNameField />
			<label htmlFor="password">{messages.setup.password.label}</label>
			<input
				id="password"
				name="password"
				type="password"
				autoComplete="new-password"
				required
				minLength={8}
				value={password}
				onChange={(event) => setPassword(event.target.value)}
				aria-describedby={describedBy}
				aria-invalid={hasError}
			/>
			<p id="password-hint">{messages.setup.password.hint}</p>
			{hasError && (
				<p id="password-error" role="alert">
					{passwordError}
				</p>
			)}
			<button type="submit" disabled={isSubmitting}>
				{messages.setup.password.submit}
			</button>
		</form>
	);
}
