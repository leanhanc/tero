import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import { App } from './app';
import { detectLanguage, LocaleContext, toLocale } from './lib/utils/locale';

const locale = toLocale(detectLanguage(navigator.languages));
document.documentElement.lang = locale.language;

const root = document.getElementById('root');
if (root) {
	createRoot(root).render(
		<StrictMode>
			<LocaleContext value={locale}>
				<App />
			</LocaleContext>
		</StrictMode>,
	);
}
