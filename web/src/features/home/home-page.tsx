import { AppShell } from '../../components/app-shell';
import { useMessages } from '../../lib/utils/locale';
import { AppList } from './components/app-list';

export function HomePage({ onLogout }: { onLogout: () => void }) {
	const messages = useMessages();

	return (
		<AppShell section="apps" onLogout={onLogout}>
			<AppList heading={messages.home.heading} />
		</AppShell>
	);
}
