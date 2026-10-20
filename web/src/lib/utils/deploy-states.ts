import type { DeployState } from './api';

const stateLabels: Record<DeployState, string> = {
	queued: 'Waiting to start',
	building: 'Building',
	starting: 'Starting',
	live: 'Live',
	failed: 'Failed',
	superseded: 'Replaced by a newer deploy',
};

const stepLabels: Record<string, string> = {
	building: 'while building',
	starting: 'while starting',
};

/** A deploy state in words, for people who don't know the internal names. */
export function describeState(state: DeployState | ''): string {
	if (state === '') {
		return 'Not deployed yet';
	}

	return stateLabels[state];
}

/** Where a failed deploy stopped, such as "while building". */
export function describeFailedStep(step: string): string {
	return stepLabels[step] ?? '';
}

/** Whether a deploy is still on its way to live or failed. */
export function isInProgress(state: DeployState | ''): boolean {
	return state === 'queued' || state === 'building' || state === 'starting';
}
