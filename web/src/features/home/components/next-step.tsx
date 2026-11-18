import type { ReactNode } from 'react';

import { TeroMark } from '../../../components/icons';

type NextStepProps = {
	heading: string;
	children: ReactNode;
};

/** Tero's guide: the one thing worth doing now, in the soft mark. */
export function NextStep({ heading, children }: NextStepProps) {
	return (
		<aside className="t-tip" aria-label={heading}>
			<div className="head">
				<span className="mark">
					<TeroMark />
				</span>
				<b>{heading}</b>
			</div>
			{children}
		</aside>
	);
}
