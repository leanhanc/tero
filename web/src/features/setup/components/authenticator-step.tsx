import { type SubmitEvent, useEffect, useRef, useState } from 'react';

import { ApiError, type Enrolment, errorMessage, finishSetup } from '../../../lib/utils/api';
import { navigate } from '../../../lib/utils/navigation';

type AuthenticatorStepProps = {
	token: string;
	enrolment: Enrolment;
	onLinkError: (message: string) => void;
};

export function AuthenticatorStep({ token, enrolment, onLinkError }: AuthenticatorStepProps) {
	const [code, setCode] = useState('');
	const [codeError, setCodeError] = useState('');
	const [isSubmitting, setIsSubmitting] = useState(false);
	const headingRef = useRef<HTMLHeadingElement>(null);

	// The password form was replaced, so focus moves to the new step's heading
	// for keyboard and screen reader users.
	useEffect(() => headingRef.current?.focus(), []);

	const submit = async (event: SubmitEvent<HTMLFormElement>) => {
		event.preventDefault();
		setIsSubmitting(true);
		setCodeError('');

		try {
			await finishSetup(token, code);
			navigate('/');
		} catch (error) {
			const isCodeProblem = error instanceof ApiError && (error.status === 422 || error.status === 429);
			if (isCodeProblem) {
				setCodeError(error.message);
			} else {
				onLinkError(errorMessage(error));
			}
		} finally {
			setIsSubmitting(false);
		}
	};

	const hasError = codeError !== '';

	return (
		<form onSubmit={submit}>
			<h2 ref={headingRef} tabIndex={-1}>
				Add Tero to your authenticator app
			</h2>
			<p>Scan this QR code with an authenticator app, such as 1Password, Bitwarden or Google Authenticator.</p>
			<img src={enrolment.qrCode} alt="QR code that adds Tero to an authenticator app" width={256} height={256} />
			<p>
				Can't scan it? Enter this key in the app instead: <code>{enrolment.secret}</code>
			</p>
			<label htmlFor="code">6-digit code from the app</label>
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
				aria-describedby={hasError ? 'code-error' : undefined}
				aria-invalid={hasError}
			/>
			{hasError && (
				<p id="code-error" role="alert">
					{codeError}
				</p>
			)}
			<button type="submit" disabled={isSubmitting}>
				Finish setup
			</button>
		</form>
	);
}
