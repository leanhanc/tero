import '@fontsource/atkinson-hyperlegible-next/400.css';
import '@fontsource/atkinson-hyperlegible-next/500.css';
import '@fontsource/atkinson-hyperlegible-next/600.css';
import '@fontsource/atkinson-hyperlegible-next/700.css';
import '@fontsource/funnel-display/600.css';
import '@fontsource/jetbrains-mono/400.css';
import './styles/index.css';

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
