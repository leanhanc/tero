import { useMessages } from '../../../lib/utils/locale';

export function SetupProblem({ message }: { message: string }) {
	const messages = useMessages();

	return (
		<main>
			<h1>{messages.setup.heading}</h1>
			<p role="alert">{message}</p>
		</main>
	);
}
