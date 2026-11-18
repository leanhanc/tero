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
		<section className="t-note err" aria-label={messages.label}>
			<span className="t-state err" aria-hidden="true" />
			<div className="stack">
				<p role="alert">{messages.changed}</p>
				{variables.isRepositoryReplaced && <p>{messages.replaced}</p>}
				<dl className="facts">
					<div>
						<dt>{messages.approved}</dt>
						<dd>{describeSource(variables.approvedSource, messages)}</dd>
					</div>
					<div>
						<dt>{messages.current}</dt>
						<dd>{describeSource(variables.source, messages)}</dd>
					</div>
				</dl>
				{isConfirming && <ConfirmForm onConfirm={confirmThenApprove} onCancel={() => setIsConfirming(false)} />}
				<div className="row">
					<button className="t-btn secondary" type="button" onClick={approve} disabled={isConfirming}>
						{messages.approve}
					</button>
				</div>
				{error && (
					<p className="t-error" role="alert">
						{error}
					</p>
				)}
			</div>
		</section>
	);
}

function describeSource(source: AppSource, messages: Messages['app']['variables']['source']): string {
	return messages.describe(source.repository, source.branch, source.rootDirectory);
}
