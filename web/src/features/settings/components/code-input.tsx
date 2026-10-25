/** A 6-digit TOTP code field that password managers and phones can fill. */
export function CodeInput({ id, value, onChange }: { id: string; value: string; onChange: (code: string) => void }) {
	return (
		<input
			id={id}
			name="code"
			type="text"
			inputMode="numeric"
			autoComplete="one-time-code"
			required
			pattern="[0-9]{6}"
			maxLength={6}
			value={value}
			onChange={(event) => onChange(event.target.value.trim())}
		/>
	);
}
