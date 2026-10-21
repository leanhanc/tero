import { PageHeader } from '../../components/page-header';
import { AppList } from './components/app-list';

export function HomePage({ onLogout }: { onLogout: () => void }) {
	return (
		<>
			<PageHeader onLogout={onLogout} />
			<main>
				<h1>Apps</h1>
				<AppList />
			</main>
		</>
	);
}
