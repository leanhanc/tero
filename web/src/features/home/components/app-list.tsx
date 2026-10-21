import { useEffect, useState } from 'react';

import { AddressHelp } from '../../../components/address-help';
import { InternalLink } from '../../../components/internal-link';
import { type AppSummary, errorMessage, listApps } from '../../../lib/utils/api';
import { describeState } from '../../../lib/utils/deploy-states';

type LoadedApps = { apps: AppSummary[] } | { error: string } | null;

export function AppList() {
	const [loaded, setLoaded] = useState<LoadedApps>(null);

	useEffect(() => {
		let isCurrent = true;
		listApps()
			.then((apps) => isCurrent && setLoaded({ apps }))
			.catch((error: unknown) => isCurrent && setLoaded({ error: errorMessage(error) }));

		return () => {
			isCurrent = false;
		};
	}, []);

	return (
		<section aria-label="Your apps">
			<p>
				<InternalLink to="/apps/new">Deploy a new app</InternalLink>
			</p>
			<AppListContent loaded={loaded} />
		</section>
	);
}

function AppListContent({ loaded }: { loaded: LoadedApps }) {
	if (loaded === null) {
		return <p>Loading…</p>;
	}

	if ('error' in loaded) {
		return <p role="alert">{loaded.error}</p>;
	}

	if (loaded.apps.length === 0) {
		return <p>No apps yet. Deploy one from a public GitHub repository to get started.</p>;
	}

	const firstAddress = loaded.apps[0]?.address ?? '';

	return (
		<table>
			<thead>
				<tr>
					<th scope="col">App</th>
					<th scope="col">
						Address <AddressHelp address={firstAddress} />
					</th>
					<th scope="col">Latest deploy</th>
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
						<td>{describeState(app.latestState)}</td>
					</tr>
				))}
			</tbody>
		</table>
	);
}
