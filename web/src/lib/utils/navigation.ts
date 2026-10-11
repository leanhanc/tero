import { useEffect, useState } from 'react';

// The dashboard has a handful of pages, so a router library would be more
// than it needs: pages are chosen from location.pathname.

const navigationEvent = 'tero:navigate';

export function navigate(path: string): void {
	window.history.pushState(null, '', path);
	window.dispatchEvent(new Event(navigationEvent));
}

export function usePath(): string {
	const [path, setPath] = useState(window.location.pathname);

	useEffect(() => {
		const updatePath = () => setPath(window.location.pathname);
		window.addEventListener('popstate', updatePath);
		window.addEventListener(navigationEvent, updatePath);

		return () => {
			window.removeEventListener('popstate', updatePath);
			window.removeEventListener(navigationEvent, updatePath);
		};
	}, []);

	return path;
}
