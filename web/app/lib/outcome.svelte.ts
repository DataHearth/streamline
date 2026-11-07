// What happened to the last action on one item (a search hit), shown on the
// button that ran it. A success shows for `hold` ms, then clears; a failure
// flashes for as long, and its reason stays until the next attempt.
export type OutcomePhase = "idle" | "pending" | "done" | "failed";

export function createOutcome<K>(hold = 1600) {
	let current = $state<{
		key: K;
		phase: "done" | "failed";
		flashing: boolean;
		error?: string;
	} | null>(null);
	let timer: ReturnType<typeof setTimeout> | undefined;

	function set(next: NonNullable<typeof current>) {
		clearTimeout(timer);
		current = next;
		timer = setTimeout(() => {
			if (!current) return;
			current = current.phase === "done" ? null : { ...current, flashing: false };
		}, hold);
	}

	return {
		done(key: K) {
			set({ key, phase: "done", flashing: true });
		},
		failed(key: K, error: string) {
			set({ key, phase: "failed", flashing: true, error });
		},
		clear() {
			clearTimeout(timer);
			current = null;
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
			return current?.key === key && current.phase === "failed" ? current.error : undefined;
		},
	};
}
