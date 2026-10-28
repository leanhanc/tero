import { useEffect, useState } from 'react';

import { InternalLink } from '../../../components/internal-link';
import { type GitHubRepository, type GitHubStatus, getGitHub } from '../../../lib/utils/api';
import { useMessages } from '../../../lib/utils/locale';

type GitHubRepositoryPickerProps = {
	selectedID: number | undefined;
	onPick: (repository: GitHubRepository) => void;
};

/**
 * The repositories the GitHub App can read, private ones included. Without
 * the App, a pointer to Settings, since public repositories work by URL.
 */
export function GitHubRepositoryPicker({ selectedID, onPick }: GitHubRepositoryPickerProps) {
	const messages = useMessages().newApp.gitHubRepository;
	const [status, setStatus] = useState<GitHubStatus | null>(null);

	useEffect(() => {
		let isCurrent = true;
		// The URL field works without it, so a failure only hides the list.
		getGitHub()
			.then((loaded) => isCurrent && setStatus(loaded))
			.catch(() => isCurrent && setStatus(null));

		return () => {
			isCurrent = false;
		};
	}, []);

	const repositories = status?.repositories ?? [];
	if (repositories.length === 0) {
		return (
			<p>
				{messages.connectHint} <InternalLink to="/settings">{messages.settingsLink}</InternalLink>.
			</p>
		);
	}

	const pick = (value: string) => {
		const picked = repositories.find((repository) => String(repository.id) === value);
		if (picked) {
			onPick(picked);
		}
	};

	return (
		<>
			<label htmlFor="github-repository">{messages.label}</label>
			<select id="github-repository" value={selectedID ?? ''} onChange={(event) => pick(event.target.value)}>
				<option value="">{messages.none}</option>
				{repositories.map((repository) => (
					<option key={repository.id} value={repository.id}>
						{repository.owner}/{repository.name}
						{repository.isPrivate ? ` (${messages.private})` : ''}
					</option>
				))}
			</select>
		</>
	);
}
