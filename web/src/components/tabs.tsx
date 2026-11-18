import { type KeyboardEvent, type ReactNode, useRef, useState } from 'react';

export type Tab<Id extends string> = { id: Id; label: string };

type TabsProps<Id extends string> = {
	/** Names the tab list for screen readers, such as the page's heading. */
	label: string;
	tabs: Tab<Id>[];
	selected: Id;
	onSelect: (id: Id) => void;
};

/**
 * A row of tabs that follows the ARIA tabs pattern: the arrow keys, Home and
 * End move between tabs, and Tab moves into the open panel.
 */
export function Tabs<Id extends string>({ label, tabs, selected, onSelect }: TabsProps<Id>) {
	const buttonsRef = useRef(new Map<Id, HTMLButtonElement>());

	const moveFocus = (event: KeyboardEvent<HTMLDivElement>) => {
		const nextIndex = indexAfterKey(event.key, tabs.findIndex((tab) => tab.id === selected), tabs.length);
		const nextTab = nextIndex === null ? undefined : tabs[nextIndex];
		if (!nextTab) {
			return;
		}

		event.preventDefault();
		onSelect(nextTab.id);
		buttonsRef.current.get(nextTab.id)?.focus();
	};

	return (
		<div className="t-tabs" role="tablist" aria-label={label} onKeyDown={moveFocus}>
			{tabs.map((tab) => {
				const isSelected = tab.id === selected;
				return (
					<button
						key={tab.id}
						ref={(button) => {
							if (button) {
								buttonsRef.current.set(tab.id, button);
							}
						}}
						className="t-tab"
						id={tabID(tab.id)}
						type="button"
						role="tab"
						aria-selected={isSelected}
						aria-controls={panelID(tab.id)}
						tabIndex={isSelected ? 0 : -1}
						onClick={() => onSelect(tab.id)}
					>
						{tab.label}
					</button>
				);
			})}
		</div>
	);
}

/** The open tab's content. */
export function TabPanel({ id, children }: { id: string; children: ReactNode }) {
	return (
		<div className="stack" role="tabpanel" id={panelID(id)} aria-labelledby={tabID(id)}>
			{children}
		</div>
	);
}

/**
 * The open tab, kept in the address as ?tab= so a reload or a shared link
 * opens the same one. An unknown value opens the first tab.
 */
export function useTab<Id extends string>(tabIDs: readonly Id[]): [Id, (id: Id) => void] {
	const [selected, setSelected] = useState<Id>(() => readTab(tabIDs));

	const select = (id: Id) => {
		setSelected(id);
		const url = new URL(window.location.href);
		url.searchParams.set('tab', id);
		window.history.replaceState(null, '', url);
	};

	return [selected, select];
}

function readTab<Id extends string>(tabIDs: readonly Id[]): Id {
	const requested = new URLSearchParams(window.location.search).get('tab');
	const known = tabIDs.find((id) => id === requested);
	return known ?? (tabIDs[0] as Id);
}

function indexAfterKey(key: string, index: number, count: number): number | null {
	const keyMoves: Record<string, number> = {
		ArrowRight: (index + 1) % count,
		ArrowLeft: (index - 1 + count) % count,
		Home: 0,
		End: count - 1,
	};

	return keyMoves[key] ?? null;
}

function tabID(id: string): string {
	return `tab-${id}`;
}

function panelID(id: string): string {
	return `panel-${id}`;
}
