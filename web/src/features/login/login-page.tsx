import { AuthCard } from '../../components/auth-card';
import { useMessages } from '../../lib/utils/locale';
import { isPasskeySupported } from '../../lib/utils/passkeys';
import { LoginForm } from './components/login-form';
import { NotSetUpNotice } from './components/not-set-up-notice';
import { PasskeyLogin } from './components/passkey-login';

type LoginPageProps = {
	isSetUp: boolean;
	onLogin: () => void;
};

export function LoginPage({ isSetUp, onLogin }: LoginPageProps) {
	const messages = useMessages();

	return (
		<AuthCard>
			<h1 className="ah">{messages.login.heading}</h1>
			{isSetUp ? <LoginMethods onLogin={onLogin} /> : <NotSetUpNotice />}
		</AuthCard>
	);
}

/** The passkey first when the browser has them, with the password and a code as the other way in. */
function LoginMethods({ onLogin }: { onLogin: () => void }) {
	const hasPasskeys = isPasskeySupported();

	return (
		<>
			{hasPasskeys && <PasskeyLogin onLogin={onLogin} />}
			<LoginForm isMainWay={!hasPasskeys} onLogin={onLogin} />
			<LoginHelp />
		</>
	);
}

/** How to get back in without the password or the authenticator app. */
function LoginHelp() {
	const messages = useMessages().login.help;

	return (
		<details className="more">
			<summary>{messages.summary}</summary>
			<p className="cap">
				{messages.beforeCommand} <code>sudo tero reset-login</code> {messages.afterCommand}
			</p>
		</details>
	);
}
