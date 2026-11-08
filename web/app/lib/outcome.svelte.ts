import { toast } from "./toast";
import { BELOW_MD } from "./viewport.svelte";

// What happened to the last action on one item (a search hit), shown for
// `hold` ms on the button that ran it. A failure's reason goes to an error
// toast from md, and below md to a bubble above the button (`error`, drawn by
// OutcomeButton), since there the bottom of the screen is the sheet's footer
// and a toast would cover its add button. The bubble goes at the first touch
// anywhere, which still reaches what lies under it, or after as long as a
// toast lasts; the next attempt or result replaces either.
export type OutcomePhase = "idle" | "pending" | "done" | "failed";

const BUBBLE_MS = 4000;

export function createOutcome<K>(hold = 1600) {
	let current = $state<{ key: K; phase: "done" | "failed"; flashing: boolean } | null>(null);
	let bubble = $state<{ key: K; error: string } | null>(null);
	let timer: ReturnType<typeof setTimeout> | undefined;
	let bubbleTimer: ReturnType<typeof setTimeout> | undefined;
	let errorToast: string | number | undefined;

	function dropBubble() {
		clearTimeout(bubbleTimer);
		window.removeEventListener("pointerdown", dropBubble, true);
		bubble = null;
	}

	function dropReason() {
		dropBubble();
		if (errorToast !== undefined) toast.dismiss(errorToast);
		errorToast = undefined;
	}

	function set(next: NonNullable<typeof current>) {
		clearTimeout(timer);
		dropReason();
		current = next;
		timer = setTimeout(() => {
			if (!current) return;
			current = current.phase === "done" ? null : { ...current, flashing: false };
		}, hold);
	}

	// Nothing outlives the component that made it.
	$effect(() => () => {
		clearTimeout(timer);
		dropBubble();
	});

	return {
		done(key: K) {
			set({ key, phase: "done", flashing: true });
		},
		failed(key: K, error: string) {
			set({ key, phase: "failed", flashing: true });
			if (!window.matchMedia(BELOW_MD).matches) {
				errorToast = toast.err(error);
				return;
			}
			bubble = { key, error };
			bubbleTimer = setTimeout(dropBubble, BUBBLE_MS);
			window.addEventListener("pointerdown", dropBubble, true);
		},
		clear() {
			clearTimeout(timer);
			current = null;
			dropReason();
		},
		phase(key: K, pending: boolean): OutcomePhase {
			if (pending) return "pending";
			if (!current || current.key !== key) return "idle";
			if (current.phase === "done") return "done";
			return current.flashing ? "failed" : "idle";
		},
		// True while the success shows: the caller keeps the button in place
		// rather than switching to the item's new state.
		showing(key: K): boolean {
			return current?.key === key && current.phase === "done";
		},
		error(key: K): string | undefined {
			return bubble?.key === key ? bubble.error : undefined;
		},
	};
}
