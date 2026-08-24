// Mounting a full library grid in one synchronous pass blocks the main
// thread for seconds (634 PosterCards ≈ 1.8s measured), freezing every click
// during route navigation. IncrementalList caps how many rows a page renders
// and grows the cap in small chunks — driven by RenderSentinel — so the first
// screen paints immediately and the rest mounts without ever blocking input.

const INITIAL = 20;
const CHUNK = 50;

export class IncrementalList<T> {
	#limit = $state(INITIAL);
	#source: () => T[];

	constructor(source: () => T[]) {
		this.#source = source;
	}

	get items(): T[] {
		const all = this.#source();
		return all.length > this.#limit ? all.slice(0, this.#limit) : all;
	}

	get pending(): boolean {
		return this.#source().length > this.#limit;
	}

	grow() {
		if (this.pending) this.#limit += CHUNK;
	}

	// Call when the filter/sort driving the source changes wholesale — slicing
	// a re-sorted list at a grown limit would remount everything at once,
	// which is the exact freeze this exists to avoid.
	reset() {
		this.#limit = INITIAL;
	}
}
