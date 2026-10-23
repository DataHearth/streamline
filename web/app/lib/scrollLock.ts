// Overlays nest — a Dialog opened from inside a TorrentDrawer is two holders of
// the same body style. Ref-counting is what stops the inner one's teardown from
// releasing the page scroll while the outer overlay is still up.
let holders = 0;

export function lockScroll() {
	holders += 1;
	if (holders === 1) document.body.style.overflow = "hidden";
}

export function unlockScroll() {
	if (holders === 0) return;
	holders -= 1;
	if (holders === 0) document.body.style.overflow = "";
}
