import { type SubmitEvent, useState } from 'react';

import { AccountNameField } from '../../../components/account-name-field';
import { confirmIdentity, errorMessage } from '../../../lib/utils/api';
import { useMessages } from '../../../lib/utils/locale';
import { CodeInput } from './code-input';

type ConfirmFormProps = {
	onConfirm: () => void;
	onCancel: () => void;
};

/**
 * Asks for the password and a code so this session may end sessions and
 * change passkeys. It appears when the admin asks for one of those actions.
 */
export function ConfirmForm({ onConfirm, onCancel }: ConfirmFormProps) {
	const messages = useMessages();
	const [password, setPassword] = useState('');
	const [code, setCode] = useState('');
	const [formError, setFormError] = useState('');
	const [isSubmitting, setIsSubmitting] = useState(false);

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
		<section aria-labelledby="confirm-heading">
			<h2 id="confirm-heading">{messages.settings.confirm.heading}</h2>
			<p id="confirm-intro">{messages.settings.confirm.intro}</p>
			<form onSubmit={submit} aria-describedby={formError ? 'confirm-intro confirm-error' : 'confirm-intro'}>
				<AccountNameField />
				<label htmlFor="confirm-password">{messages.settings.confirm.passwordLabel}</label>
				<input
					id="confirm-password"
					name="password"
					type="password"
					autoComplete="current-password"
					required
					value={password}
					onChange={(event) => setPassword(event.target.value)}
				/>
				<label htmlFor="confirm-code">{messages.settings.confirm.codeLabel}</label>
				<CodeInput id="confirm-code" value={code} onChange={setCode} />
				{formError && (
					<p id="confirm-error" role="alert">
						{formError}
					</p>
				)}
				<button type="submit" disabled={isSubmitting}>
					{messages.settings.confirm.submit}
				</button>
				<button type="button" onClick={onCancel}>
					{messages.settings.confirm.cancel}
				</button>
			</form>
		</section>
	);
}
