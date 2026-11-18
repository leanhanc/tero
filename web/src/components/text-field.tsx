type TextFieldProps = {
	id: string;
	label: string;
	value: string;
	onChange: (value: string) => void;
	onBlur?: () => void;
	hint?: string;
	error: string;
	isRequired?: boolean;
	/** Shows the value in the monospace font, for URLs, branches and paths. */
	isCode?: boolean;
	placeholder?: string;
	/** Text that follows the value, such as the domain after an app's address. */
	suffix?: string;
};

/** A labelled text input with an optional hint and its own error. */
export function TextField({ id, label, value, onChange, onBlur, hint, error, isRequired, isCode, placeholder, suffix }: TextFieldProps) {
	const hintID = `${id}-hint`;
	const errorID = `${id}-error`;
	const hasError = error !== '';
	const describedBy = [hint ? hintID : '', hasError ? errorID : ''].filter(Boolean).join(' ');

	const input = (
		<input
			className={isCode ? 't-input code' : 't-input'}
			id={id}
			name={id}
			type="text"
			autoComplete="off"
			spellCheck={false}
			required={isRequired}
			placeholder={placeholder}
			value={value}
			onChange={(event) => onChange(event.target.value)}
			onBlur={onBlur}
			aria-describedby={describedBy || undefined}
			aria-invalid={hasError}
		/>
	);

	return (
		<div className="t-field">
			<label htmlFor={id}>{label}</label>
			{suffix ? (
				<div className="addr">
					{input}
					<span className="suffix" aria-hidden="true">
						{suffix}
					</span>
				</div>
			) : (
				input
			)}
			{hint && (
				<span className="t-help" id={hintID}>
					{hint}
				</span>
			)}
			{hasError && (
				<span className="t-error" id={errorID} role="alert">
					{error}
				</span>
			)}
		</div>
	);
}
