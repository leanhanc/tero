import { PageHeader } from '../../components/page-header';
import { useMessages } from '../../lib/utils/locale';
import { navigate } from '../../lib/utils/navigation';
import { NewAppForm } from './components/new-app-form';

export function NewAppPage({ onLogout }: { onLogout: () => void }) {
	const messages = useMessages();

	return (
		<>
			<PageHeader onLogout={onLogout} />
			<main>
				<h1>{messages.newApp.heading}</h1>
				<p>{messages.newApp.intro}</p>
				<NewAppForm onCreated={(name) => navigate(`/apps/${name}`)} />
			</main>
		</>
	);
}
