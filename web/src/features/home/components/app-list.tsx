import { useEffect, useState } from 'react';

import { AddressHelp } from '../../../components/address-help';
import { InternalLink } from '../../../components/internal-link';
import { type AppSummary, errorMessage, listApps } from '../../../lib/utils/api';
import { describeState } from '../../../lib/utils/deploy-states';
import { useMessages } from '../../../lib/utils/locale';

type LoadedApps = { apps: AppSummary[] } | { error: string } | null;

export function AppList() {
	const messages = useMessages();
	const [loaded, setLoaded] = useState<LoadedApps>(null);

	useEffect(() => {
		let isCurrent = true;
		listApps()
			.then((apps) => isCurrent && setLoaded({ apps }))
			.catch((error: unknown) => isCurrent && setLoaded({ error: errorMessage(error, messages.errors) }));

		return () => {
			isCurrent = false;
		};
	}, [messages]);

	return (
		<section aria-label={messages.home.apps.label}>
			<p>
				<InternalLink to="/apps/new">{messages.home.apps.deployNew}</InternalLink>
			</p>
			<AppListContent loaded={loaded} />
		</section>
	);
}

function AppListContent({ loaded }: { loaded: LoadedApps }) {
	const messages = useMessages();

	if (loaded === null) {
		return <p>{messages.common.loading}</p>;
	}

	if ('error' in loaded) {
		return <p role="alert">{loaded.error}</p>;
	}

	if (loaded.apps.length === 0) {
		return <p>{messages.home.apps.empty}</p>;
	}

	const firstAddress = loaded.apps[0]?.address ?? '';

	return (
		<table>
			<thead>
				<tr>
					<th scope="col">{messages.home.apps.appColumn}</th>
					<th scope="col">
						{messages.home.apps.addressColumn} <AddressHelp address={firstAddress} />
					</th>
					<th scope="col">{messages.home.apps.latestDeployColumn}</th>
				</tr>
			</thead>
			<tbody>
				{loaded.apps.map((app) => (
					<tr key={app.name}>
						<td>
							<InternalLink to={`/apps/${app.name}`}>{app.displayName}</InternalLink>
						</td>
						<td>
							<a href={app.address} rel="noreferrer" target="_blank">
								{app.address}
							</a>
						</td>
						<td>{describeState(app.latestState, messages.deployStates)}</td>
					</tr>
				))}
			</tbody>
		</table>
	);
}
