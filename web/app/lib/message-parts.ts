// Split a whole translated sentence around one slot, so the slot can be
// rendered with its own markup (a highlighted status word) without cutting the
// sentence into fragments that each language would have to order the same way.
//
//   const [pre, post] = around((status) => i18n.some_message({ status }));
//   {pre}<b>{i18n.lc_wanted()}</b>{post}

const MARK = "\u0000";

export function around(render: (slot: string) => string): [string, string] {
	const s = render(MARK);
	const i = s.indexOf(MARK);
	return i < 0 ? [s, ""] : [s.slice(0, i), s.slice(i + MARK.length)];
}

// The same for a sentence with several marked-up slots. Segments come back in
// the translation's own order, so a language that puts the slots the other way
// round still gets each one rendered with its own markup:
//
//   const body = parts(i18n.some_message, ["title", "status"]);
//   {#each body as p}{#if p.slot === "title"}<b>{x}</b>{:else if p.slot}…{:else}{p.text}{/if}{/each}
export type Part<K extends string> = { text: string; slot?: undefined } | { slot: K; text?: undefined };

export function parts<K extends string>(
	render: (inputs: Record<K, string>) => string,
	slots: readonly K[],
): Part<K>[] {
	const inputs = Object.fromEntries(slots.map((k) => [k, `${MARK}${k}${MARK}`])) as Record<K, string>;
	const out: Part<K>[] = [];
	render(inputs)
		.split(MARK)
		.forEach((seg, i) => {
			if (i % 2 === 1) out.push({ slot: seg as K });
			else if (seg) out.push({ text: seg });
		});
	return out;
}
