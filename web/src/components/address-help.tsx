import { useId, useState } from 'react';

/**
 * A "?" button that explains the nip.io part of an app's address. Shows
 * nothing for addresses on another domain, such as the dev VM's localhost.
 */
export function AddressHelp({ address }: { address: string }) {
	const [isOpen, setIsOpen] = useState(false);
	const explanationID = useId();

	const isNipAddress = address.endsWith('.nip.io');
	if (!isNipAddress) {
		return null;
	}

	return (
		<>
			<button
				type="button"
				aria-label="What is nip.io?"
				aria-expanded={isOpen}
				aria-controls={explanationID}
				onClick={() => setIsOpen((wasOpen) => !wasOpen)}
			>
				?
			</button>
			{isOpen && (
				<p id={explanationID}>
					nip.io is a free public DNS service. It answers a name with an IP address in it, such as
					shop.203.0.113.10.nip.io, with that IP, so your app gets a working HTTPS address before you set up a
					domain of your own. Visitors connect straight to your server; no traffic goes through nip.io.
				</p>
			)}
		</>
	);
}
