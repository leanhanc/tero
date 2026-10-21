import { useState } from 'react';

import type { Enrolment } from '../../lib/utils/api';
import { useMessages } from '../../lib/utils/locale';
import { AuthenticatorStep } from './components/authenticator-step';
import { PasswordStep } from './components/password-step';
import { SetupProblem } from './components/setup-problem';

// The setup link carries its one-time token in the URL fragment, which
// browsers never send to the server.
function readSetupToken(): string {
	return window.location.hash.slice(1);
}

/**
 * First login: choose a password, then add Tero to an authenticator app and
 * confirm a code. The account only exists once the code is confirmed.
 */
export function SetupPage() {
	const messages = useMessages();
	const token = readSetupToken();
	const [enrolment, setEnrolment] = useState<Enrolment | null>(null);
	const [linkError, setLinkError] = useState('');

	if (!token) {
		return <SetupProblem message={messages.setup.missingToken} />;
	}

	if (linkError) {
		return <SetupProblem message={linkError} />;
	}

	return (
		<main>
			<h1>{messages.setup.heading}</h1>
			{enrolment === null ? (
				<PasswordStep token={token} onEnrolment={setEnrolment} onLinkError={setLinkError} />
			) : (
				<AuthenticatorStep token={token} enrolment={enrolment} onLinkError={setLinkError} />
			)}
		</main>
	);
}
