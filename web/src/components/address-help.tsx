import { useId, useState } from 'react';

import { useMessages } from '../lib/utils/locale';

/**
 * A "?" button that explains the nip.io part of an app's address. Shows
 * nothing for addresses on another domain, such as the dev VM's localhost.
 */
export function AddressHelp({ address }: { address: string }) {
	const messages = useMessages();
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
				aria-label={messages.addressHelp.label}
				aria-expanded={isOpen}
				aria-controls={explanationID}
				onClick={() => setIsOpen((wasOpen) => !wasOpen)}
			>
				?
			</button>
			{isOpen && (
				<p id={explanationID}>{messages.addressHelp.explanation}</p>
			)}
		</>
	);
}
