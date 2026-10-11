import { useEffect, useState } from 'react';

import { errorMessage, getEvents, type SecurityEvent } from '../../../lib/utils/api';
import { describeEvent } from '../event-descriptions';

type LoadedEvents = { events: SecurityEvent[] } | { error: string } | null;

export function SecurityEvents() {
	const [loaded, setLoaded] = useState<LoadedEvents>(null);

	useEffect(() => {
		let isCurrent = true;
		getEvents()
			.then((events) => isCurrent && setLoaded({ events }))
			.catch((error: unknown) => isCurrent && setLoaded({ error: errorMessage(error) }));

		return () => {
			isCurrent = false;
		};
	}, []);

	return (
		<section aria-labelledby="events-heading">
			<h2 id="events-heading">Security events</h2>
			<SecurityEventsContent loaded={loaded} />
		</section>
	);
}

function SecurityEventsContent({ loaded }: { loaded: LoadedEvents }) {
	if (loaded === null) {
		return <p>Loading…</p>;
	}

	if ('error' in loaded) {
		return <p role="alert">{loaded.error}</p>;
	}

	if (loaded.events.length === 0) {
		return <p>No security events yet.</p>;
	}

	return (
		<table>
			<caption>Logins, failed attempts and login resets, newest first</caption>
			<thead>
				<tr>
					<th scope="col">Time</th>
					<th scope="col">Event</th>
					<th scope="col">From</th>
				</tr>
			</thead>
			<tbody>
				{loaded.events.map((event, index) => (
					<tr key={`${event.at}-${index}`}>
						<td>
							<time dateTime={event.at}>{new Date(event.at).toLocaleString()}</time>
						</td>
						<td>{describeEvent(event.type)}</td>
						<td>{event.ip ?? 'The server'}</td>
					</tr>
				))}
			</tbody>
		</table>
	);
}
