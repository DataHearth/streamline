import { getContext, setContext } from "svelte";

// Text that opens inside a detail hero (ClampedText's Read more) holds the
// hero's backdrop at the height it had; object-cover would rescale it.
export class HeroHold {
	height = $state<number | null>(null);
	// Set by the HeroBackdrop while it is mounted.
	measure: () => number | null = () => null;
	#open = 0;

	// Called before the text grows, so the measure is the collapsed one.
	open() {
		if (this.#open++ === 0) this.height = this.measure();
	}

	close() {
		if (this.#open > 0 && --this.#open === 0) this.height = null;
	}
}

const HERO_HOLD = Symbol("hero-hold");

export function provideHeroHold() {
	setContext(HERO_HOLD, new HeroHold());
}

export function heroHold(): HeroHold | undefined {
	return getContext<HeroHold | undefined>(HERO_HOLD);
}
