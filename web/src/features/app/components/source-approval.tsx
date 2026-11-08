import { useState } from 'react';

import { ConfirmForm } from '../../../components/confirm-form';
import {
	type AppSource,
	type AppVariables,
	approveSource,
	errorMessage,
	isConfirmationRequired,
} from '../../../lib/utils/api';
import { useMessages } from '../../../lib/utils/locale';
import type { Messages } from '../../../locales/en';

type SourceApprovalProps = {
	appName: string;
	variables: AppVariables;
	onApprove: () => void;
};

/**
 * An app's variables only go to deploys of the source approved for them. When
 * the app's source no longer matches, this shows both and asks for a fresh check
 * to approve the new one.
 */
export function SourceApproval({ appName, variables, onApprove }: SourceApprovalProps) {
	const allMessages = useMessages();
	const messages = allMessages.app.variables.source;
	const [error, setError] = useState('');
	const [isConfirming, setIsConfirming] = useState(false);

	if (variables.isSourceApproved) {
		return null;
	}

	const approve = async () => {
		setError('');
		try {
			await approveSource(appName);
			onApprove();
		} catch (approveError) {
			if (isConfirmationRequired(approveError)) {
				setIsConfirming(true);
				return;
			}
			setError(errorMessage(approveError, allMessages.errors));
		}
	};

	const confirmThenApprove = () => {
		setIsConfirming(false);
		void approve();
	};

	return (
		<section aria-label={messages.label}>
			<p role="alert">{messages.changed}</p>
			<dl>
				<dt>{messages.approved}</dt>
				<dd>{describeSource(variables.approvedSource, messages)}</dd>
				<dt>{messages.current}</dt>
				<dd>{describeSource(variables.source, messages)}</dd>
			</dl>
			{isConfirming && <ConfirmForm onConfirm={confirmThenApprove} onCancel={() => setIsConfirming(false)} />}
			<button type="button" onClick={approve} disabled={isConfirming}>
				{messages.approve}
			</button>
			{error && <p role="alert">{error}</p>}
		</section>
	);
}

function describeSource(source: AppSource, messages: Messages['app']['variables']['source']): string {
	return messages.describe(source.repository, source.branch, source.rootDirectory);
}
