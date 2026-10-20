import { PageHeader } from '../../components/page-header';
import { navigate } from '../../lib/utils/navigation';
import { NewAppForm } from './components/new-app-form';

export function NewAppPage({ onLogout }: { onLogout: () => void }) {
	return (
		<>
			<PageHeader onLogout={onLogout} />
			<main>
				<h1>Deploy a new app</h1>
				<p>Tero builds the app from a public GitHub repository and gives it its own HTTPS address.</p>
				<NewAppForm onCreated={(name) => navigate(`/apps/${name}`)} />
			</main>
		</>
	);
}
