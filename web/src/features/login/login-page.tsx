import { useMessages } from '../../lib/utils/locale';
import { LoginForm } from './components/login-form';
import { NotSetUpNotice } from './components/not-set-up-notice';

type LoginPageProps = {
	isSetUp: boolean;
	onLogin: () => void;
};

export function LoginPage({ isSetUp, onLogin }: LoginPageProps) {
	const messages = useMessages();

	return (
		<main>
			<h1>{messages.login.heading}</h1>
			{isSetUp ? <LoginForm onLogin={onLogin} /> : <NotSetUpNotice />}
		</main>
	);
}
