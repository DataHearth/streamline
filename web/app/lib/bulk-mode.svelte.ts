// Whether a touch bulk selection is live.
//
// On a phone the bulk bar takes the bottom bar's place rather than stacking
// above it — two 60px bars leave nothing to select from — so BottomNav has to
// know that something else owns the bottom of the screen. Set by BulkTouchBar
// while it is mounted; nothing else writes it.

let active = $state(false);

export const bulkMode = {
	get active(): boolean {
		return active;
	},
	set(v: boolean) {
		active = v;
	},
};
