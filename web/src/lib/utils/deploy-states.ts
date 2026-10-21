import type { Messages } from '../../locales/en';
import type { DeployState } from './api';

type DeployStateMessages = Messages['deployStates'];

/** A deploy state in words, for people who don't know the internal names. */
export function describeState(state: DeployState | '', messages: DeployStateMessages): string {
	if (state === '') {
		return messages.notDeployed;
	}

	return messages.states[state];
}

/** Where a failed deploy stopped, such as "while building". */
export function describeFailedStep(step: string, messages: DeployStateMessages): string {
	const isKnownStep = Object.hasOwn(messages.failedSteps, step);
	if (!isKnownStep) {
		return '';
	}

	return messages.failedSteps[step as keyof DeployStateMessages['failedSteps']];
}

/** Whether a deploy is still on its way to live or failed. */
export function isInProgress(state: DeployState | ''): boolean {
	return state === 'queued' || state === 'building' || state === 'starting';
}
