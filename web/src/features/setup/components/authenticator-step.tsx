import { type SubmitEvent, useEffect, useRef, useState } from 'react';

import { CodeInput } from '../../../components/code-input';
import { ApiError, type Enrolment, errorMessage, finishSetup } from '../../../lib/utils/api';
import { useMessages } from '../../../lib/utils/locale';
import { navigate } from '../../../lib/utils/navigation';
import { requestPasskeyOffer } from '../../../lib/utils/passkeys';

type AuthenticatorStepProps = {
	token: string;
	enrolment: Enrolment;
	onLinkError: (message: string) => void;
};

export function AuthenticatorStep({ token, enrolment, onLinkError }: AuthenticatorStepProps) {
	const messages = useMessages();
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
			requestPasskeyOffer();
			navigate('/');
		} catch (error) {
			const isCodeProblem = error instanceof ApiError && (error.status === 422 || error.status === 429);
			if (isCodeProblem) {
				setCodeError(errorMessage(error, messages.errors));
			} else {
				onLinkError(errorMessage(error, messages.errors));
			}
		} finally {
			setIsSubmitting(false);
		}
	};

	const hasError = codeError !== '';
	const describedBy = hasError ? 'code-hint code-error' : 'code-hint';

	return (
		<form onSubmit={submit}>
			<p className="stepn">{messages.setup.step(2, 2)}</p>
			<h1 className="ah" ref={headingRef} tabIndex={-1}>
				{messages.setup.authenticator.heading}
			</h1>
			<p className="muted">{messages.setup.authenticator.scanInstructions}</p>
			<p className="cap">{messages.setup.authenticator.appHint}</p>
			<img className="qr" src={enrolment.qrCode} alt={messages.setup.authenticator.qrCodeAlt} width={168} height={168} />
			<details className="more">
				<summary>{messages.setup.authenticator.manualKey}</summary>
				<p className="cap">{messages.setup.authenticator.manualKeyIntro}</p>
				<code className="key">{enrolment.secret}</code>
			</details>
			<div className="t-field">
				<label htmlFor="code">{messages.setup.authenticator.codeLabel}</label>
				<CodeInput id="code" value={code} onChange={setCode} describedBy={describedBy} isInvalid={hasError} />
				<span className="t-help" id="code-hint">
					{messages.common.codeHint}
				</span>
				{hasError && (
					<span className="t-error" id="code-error" role="alert">
						{codeError}
					</span>
				)}
			</div>
			<button className="t-btn primary wide" type="submit" disabled={isSubmitting}>
				{messages.setup.authenticator.submit}
			</button>
		</form>
	);
}
