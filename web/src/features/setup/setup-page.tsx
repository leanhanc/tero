import { useState } from 'react';

import type { Enrolment } from '../../lib/utils/api';
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
	const token = readSetupToken();
	const [enrolment, setEnrolment] = useState<Enrolment | null>(null);
	const [linkError, setLinkError] = useState('');

	if (!token) {
		return <SetupProblem message="This page needs the setup link printed on the server. Run sudo tero reset-login there for a new one." />;
	}

	if (linkError) {
		return <SetupProblem message={linkError} />;
	}

	return (
		<main>
			<h1>Set up your dashboard login</h1>
			{enrolment === null ? (
				<PasswordStep token={token} onEnrolment={setEnrolment} onLinkError={setLinkError} />
			) : (
				<AuthenticatorStep token={token} enrolment={enrolment} onLinkError={setLinkError} />
			)}
		</main>
	);
}
