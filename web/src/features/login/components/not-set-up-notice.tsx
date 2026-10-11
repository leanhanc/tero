export function NotSetUpNotice() {
	return (
		<p>
			The dashboard login isn't set up yet. Open the setup link printed on the server by <code>sudo tero init</code>, or run{' '}
			<code>sudo tero reset-login</code> there for a new one.
		</p>
	);
}
