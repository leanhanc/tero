import { type SubmitEvent, useState } from 'react';

import { AccountNameField } from '../../../components/account-name-field';
import { PasswordInput } from '../../../components/password-input';
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
			<p className="stepn">{messages.setup.step(1, 2)}</p>
			<h1 className="ah">{messages.setup.heading}</h1>
			<h2 className="h2">{messages.setup.password.heading}</h2>
			<AccountNameField />
			<div className="t-field">
				<label htmlFor="password">{messages.setup.password.label}</label>
				<PasswordInput
					id="password"
					name="password"
					autoComplete="new-password"
					minLength={8}
					value={password}
					onChange={setPassword}
					describedBy={describedBy}
					isInvalid={hasError}
				/>
				<span className="t-help" id="password-hint">
					{messages.setup.password.hint}
				</span>
				{hasError && (
					<span className="t-error" id="password-error" role="alert">
						{passwordError}
					</span>
				)}
			</div>
			<button className="t-btn primary wide" type="submit" disabled={isSubmitting}>
				{messages.setup.password.submit}
			</button>
		</form>
	);
}
