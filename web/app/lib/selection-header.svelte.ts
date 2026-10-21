// The phone's selection header: while a touch list is selecting, the top bar
// stops being the page title and becomes the selection — close, count, select
// all — the way Gmail, Photos and iOS Mail turn their app bar over to it.
//
// It used to be a band of its own pinned under the top bar, which pushed the
// list down as selecting began and sat between the rounded cards as a strip
// that belonged to neither. SelectionTopBar claims this while it is mounted
// (the pages still place it where their old band was); TopBar renders it.

import type { Noun } from "./nouns";

export type SelectionHeaderState = {
	count: number;
	total: number;
	noun: Noun;
	onClear: () => void;
	onSelectAll: () => void;
};

let current = $state<(() => SelectionHeaderState) | null>(null);

export const selectionHeader = {
	// Read through the getter so the bar follows the page's live count.
	get state(): SelectionHeaderState | null {
		return current?.() ?? null;
	},
	// Returns the release. A later claim wins; releasing a claim that has
	// since been replaced leaves the newer one alone.
	claim(read: () => SelectionHeaderState): () => void {
		current = read;
		return () => {
			if (current === read) current = null;
		};
	},
};
