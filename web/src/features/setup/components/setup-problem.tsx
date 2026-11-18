import { AuthCard } from '../../../components/auth-card';
import { useMessages } from '../../../lib/utils/locale';

export function SetupProblem({ message }: { message: string }) {
	const messages = useMessages();

	return (
		<AuthCard>
			<h1 className="ah">{messages.setup.heading}</h1>
			<div className="t-note err" role="alert">
				<span className="t-state err" aria-hidden="true" />
				<div>
					<p>{message}</p>
				</div>
			</div>
		</AuthCard>
	);
}
