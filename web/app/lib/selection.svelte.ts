// A bulk selection over a list keyed by K (a torrent hash, a record id).
//
// Held as keys rather than rows so a 2-second refetch can't strand a stale
// copy in the set, and reassigned on every change since Set mutation isn't
// reactive. `selecting` is the phone's selection mode: a touch list has no
// checkbox column, so once a long press turns it on every tap toggles a row
// instead of opening it, until the selection is cleared.

export class Selection<K> {
	keys = $state(new Set<K>());
	#mode = $state(false);

	get size(): number {
		return this.keys.size;
	}
	get selecting(): boolean {
		return this.#mode || this.keys.size > 0;
	}

	has(key: K): boolean {
		return this.keys.has(key);
	}
	toggle(key: K, on: boolean = !this.keys.has(key)) {
		const next = new Set(this.keys);
		if (on) next.add(key);
		else next.delete(key);
		this.keys = next;
	}
	// The long press: selection mode on, and the held row with it.
	begin(key: K) {
		this.#mode = true;
		this.toggle(key, true);
	}
	selectAll(keys: K[]) {
		this.keys = new Set(keys);
	}
	clear() {
		this.#mode = false;
		if (this.keys.size > 0) this.keys = new Set();
	}
	// Drops keys whose row has gone — a cancelled download, a torrent removed
	// elsewhere — so the count never claims rows that are no longer listed.
	prune(present: K[]) {
		if (this.keys.size === 0) return;
		const live = new Set(present);
		const kept = [...this.keys].filter((k) => live.has(k));
		if (kept.length !== this.keys.size) this.keys = new Set(kept);
	}
}
