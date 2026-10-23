const FOCUSABLE =
	'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

export function focusableIn(node: HTMLElement): HTMLElement[] {
	return Array.from(node.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
		(el) => el.offsetParent !== null || el === document.activeElement,
	);
}

// Initial-focus priority: [data-autofocus] (explicit opt-in for non-input
// targets like a Dialog's Cancel) → first form field → first focusable.
export function initialFocusTarget(node: HTMLElement): HTMLElement | null {
	const explicit = node.querySelector<HTMLElement>(
		"[data-autofocus]:not([disabled])",
	);
	if (explicit) return explicit;
	// Readonly fields are skipped too: on a read-only instance every field is
	// one, and focusing it plants a caret and a focus ring on something that
	// can't be typed into.
	const field = node.querySelector<HTMLElement>(
		"input:not([disabled]):not([readonly]), select:not([disabled]), textarea:not([disabled]):not([readonly])",
	);
	return field ?? focusableIn(node)[0] ?? null;
}

// Svelte action. Extracted from Modal so the full-screen config form shares one
// implementation with it — a surface that covers the whole viewport still has
// the background page's controls in the tab order without this.
export function trapFocus(node: HTMLElement) {
	function onKeydown(e: KeyboardEvent) {
		if (e.key !== "Tab") return;
		const els = focusableIn(node);
		if (els.length === 0) return;
		// Non-null: els is non-empty per the guard above, so both ends exist.
		const first = els[0]!;
		const last = els[els.length - 1]!;
		const active = document.activeElement as HTMLElement | null;
		if (e.shiftKey && (active === first || !node.contains(active))) {
			e.preventDefault();
			last.focus();
		} else if (!e.shiftKey && (active === last || !node.contains(active))) {
			e.preventDefault();
			first.focus();
		}
	}
	node.addEventListener("keydown", onKeydown);
	return {
		destroy() {
			node.removeEventListener("keydown", onKeydown);
		},
	};
}

// Re-home an overlay on <body> so a fixed backdrop is never clipped or
// neutralised by a transformed / pointer-events-none ancestor (e.g. a card's
// hover overlay). Lets a dialog be declared anywhere in the tree.
export function portal(node: HTMLElement) {
	document.body.appendChild(node);
	return {
		destroy() {
			node.parentNode?.removeChild(node);
		},
	};
}
