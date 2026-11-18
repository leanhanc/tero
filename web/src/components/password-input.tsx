import { type Ref, useState } from 'react';

import { useMessages } from '../lib/utils/locale';
import { EyeIcon, EyeSlashIcon } from './icons';

type PasswordInputProps = {
	id: string;
	name: string;
	autoComplete: 'current-password' | 'new-password';
	value: string;
	onChange: (password: string) => void;
	minLength?: number;
	describedBy?: string;
	isInvalid?: boolean;
	inputRef?: Ref<HTMLInputElement>;
};

/** A password field with a button that shows or hides what was typed. */
export function PasswordInput({ id, name, autoComplete, value, onChange, minLength, describedBy, isInvalid, inputRef }: PasswordInputProps) {
	const messages = useMessages().common;
	const [isShown, setIsShown] = useState(false);

	return (
		<div className="t-pass">
			<input
				ref={inputRef}
				className="t-input"
				id={id}
				name={name}
				type={isShown ? 'text' : 'password'}
				autoComplete={autoComplete}
				spellCheck={false}
				required
				minLength={minLength}
				value={value}
				onChange={(event) => onChange(event.target.value)}
				aria-describedby={describedBy}
				aria-invalid={isInvalid}
			/>
			<button
				type="button"
				aria-pressed={isShown}
				aria-label={isShown ? messages.hidePassword : messages.showPassword}
				onClick={() => setIsShown((wasShown) => !wasShown)}
			>
				{isShown ? <EyeSlashIcon /> : <EyeIcon />}
			</button>
		</div>
	);
}
