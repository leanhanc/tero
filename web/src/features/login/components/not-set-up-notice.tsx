import { useMessages } from '../../../lib/utils/locale';

export function NotSetUpNotice() {
	const messages = useMessages().login.notSetUp;

	return (
		<p>
			{messages.beforeInit} <code>sudo tero init</code>
			{messages.beforeReset} <code>sudo tero reset-login</code> {messages.afterReset}
		</p>
	);
}
