type TextFieldProps = {
	id: string;
	label: string;
	value: string;
	onChange: (value: string) => void;
	onBlur?: () => void;
	hint?: string;
	error: string;
	isRequired?: boolean;
	placeholder?: string;
};

/** A labelled text input with an optional hint and its own error. */
export function TextField({ id, label, value, onChange, onBlur, hint, error, isRequired, placeholder }: TextFieldProps) {
	const hintID = `${id}-hint`;
	const errorID = `${id}-error`;
	const hasError = error !== '';
	const describedBy = [hint ? hintID : '', hasError ? errorID : ''].filter(Boolean).join(' ');

	return (
		<>
			<label htmlFor={id}>{label}</label>
			<input
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
			{hint && <p id={hintID}>{hint}</p>}
			{hasError && (
				<p id={errorID} role="alert">
					{error}
				</p>
			)}
		</>
	);
}
