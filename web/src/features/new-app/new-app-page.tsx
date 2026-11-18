import { AppShell } from '../../components/app-shell';
import { InternalLink } from '../../components/internal-link';
import { useMessages } from '../../lib/utils/locale';
import { navigate } from '../../lib/utils/navigation';
import { NewAppForm } from './components/new-app-form';

export function NewAppPage({ onLogout }: { onLogout: () => void }) {
	const messages = useMessages();

	return (
		<AppShell section="apps" onLogout={onLogout}>
			<p className="crumb">
				<InternalLink to="/" className="t-link">
					{messages.home.heading}
				</InternalLink>{' '}
				/ {messages.newApp.crumb}
			</p>
			<div className="ph">
				<h1>{messages.newApp.heading}</h1>
			</div>
			<p className="muted lead">{messages.newApp.intro}</p>
			<NewAppForm onCreated={(name) => navigate(`/apps/${name}`)} onCancel={() => navigate('/')} />
		</AppShell>
	);
}
