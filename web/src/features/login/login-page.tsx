import { LoginForm } from './components/login-form';
import { NotSetUpNotice } from './components/not-set-up-notice';

type LoginPageProps = {
	isSetUp: boolean;
	onLogin: () => void;
};

export function LoginPage({ isSetUp, onLogin }: LoginPageProps) {
	return (
		<main>
			<h1>Log in to Tero</h1>
			{isSetUp ? <LoginForm onLogin={onLogin} /> : <NotSetUpNotice />}
		</main>
	);
}
