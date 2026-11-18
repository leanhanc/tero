import type { MouseEvent, ReactNode } from 'react';

import { navigate } from '../lib/utils/navigation';

type InternalLinkProps = {
	to: string;
	className?: string;
	/** Marks the link as the page that is open, for the menu. */
	isCurrent?: boolean;
	children: ReactNode;
};

/**
 * A link to another dashboard page that changes pages without a reload.
 * Clicks with a modifier key keep the browser's behaviour, such as opening a
 * new tab.
 */
export function InternalLink({ to, className, isCurrent = false, children }: InternalLinkProps) {
	const followLink = (event: MouseEvent<HTMLAnchorElement>) => {
		const hasModifier = event.metaKey || event.ctrlKey || event.shiftKey || event.altKey;
		if (hasModifier || event.button !== 0) {
			return;
		}

		event.preventDefault();
		navigate(to);
	};

	return (
		<a href={to} className={className} aria-current={isCurrent ? 'page' : undefined} onClick={followLink}>
			{children}
		</a>
	);
}
