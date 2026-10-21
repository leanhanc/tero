import { PageHeader } from '../../components/page-header';
import { useMessages } from '../../lib/utils/locale';
import { AppList } from './components/app-list';

export function HomePage({ onLogout }: { onLogout: () => void }) {
	const messages = useMessages();

	return (
		<>
			<PageHeader onLogout={onLogout} />
			<main>
				<h1>{messages.home.heading}</h1>
				<AppList />
			</main>
		</>
	);
}
