import { PageHeader } from '../../components/page-header';
import { AppList } from './components/app-list';
import { SecurityEvents } from './components/security-events';

export function HomePage({ onLogout }: { onLogout: () => void }) {
	return (
		<>
			<PageHeader onLogout={onLogout} />
			<main>
				<h1>Apps</h1>
				<AppList />
				<SecurityEvents />
			</main>
		</>
	);
}
