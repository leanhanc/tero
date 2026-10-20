import type { MouseEvent, ReactNode } from 'react';

import { navigate } from '../lib/utils/navigation';

/**
 * A link to another dashboard page that changes pages without a reload.
 * Clicks with a modifier key keep the browser's behaviour, such as opening a
 * new tab.
 */
export function InternalLink({ to, children }: { to: string; children: ReactNode }) {
	const followLink = (event: MouseEvent<HTMLAnchorElement>) => {
		const hasModifier = event.metaKey || event.ctrlKey || event.shiftKey || event.altKey;
		if (hasModifier || event.button !== 0) {
			return;
		}

		event.preventDefault();
		navigate(to);
	};

	return (
		<a href={to} onClick={followLink}>
			{children}
		</a>
	);
}
