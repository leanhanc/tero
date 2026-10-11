export function SetupProblem({ message }: { message: string }) {
	return (
		<main>
			<h1>Set up your dashboard login</h1>
			<p role="alert">{message}</p>
		</main>
	);
}
