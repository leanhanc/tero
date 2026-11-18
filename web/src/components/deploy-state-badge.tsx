import type { DeployState } from '../lib/utils/api';
import { describeState, stateTone } from '../lib/utils/deploy-states';
import { useMessages } from '../lib/utils/locale';

/** A deploy state as icon, colour and word, so it never relies on colour alone. */
export function DeployStateBadge({ state }: { state: DeployState | '' }) {
	const messages = useMessages();

	return <span className={`t-state ${stateTone(state)}`}>{describeState(state, messages.deployStates)}</span>;
}
