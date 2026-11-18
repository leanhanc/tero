import type { ReactNode } from 'react';

import { Brand } from './brand';

/** The centred card the pages before signing in sit in, with the logo on top. */
export function AuthCard({ children }: { children: ReactNode }) {
	return (
		<main className="auth">
			<div className="box">
				<Brand isMarkOnly />
				{children}
			</div>
		</main>
	);
}
