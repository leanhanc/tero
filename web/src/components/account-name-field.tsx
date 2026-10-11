/**
 * Password managers save a password under a username. There is only one
 * account, so it gets a fixed one the admin never types.
 */
export function AccountNameField() {
	return (
		<input
			type="text"
			name="username"
			autoComplete="username"
			value={`admin@${window.location.hostname}`}
			readOnly
			hidden
		/>
	);
}
