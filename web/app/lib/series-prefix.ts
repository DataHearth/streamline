// A row of posters that holds several titles of one franchise reads "Hunger
// Games : L…" on every card once each title is cut to its card's width: what
// tells them apart is at the end. splitSeriesPrefixes finds the part a title
// shares with others of the same row so the card can set it apart and lead
// with the rest ("Hunger Games" over "La Révolte : Partie 2").
//
// A prefix ends on a separator: a colon, spaced as French (" : ") or English
// (": ") spells it, or a spaced dash. Titles are grouped by what comes before
// their first separator, and a group of two or more takes the longest prefix
// all of its titles share, cut back to the last separator inside it. Taking the
// first separator alone would split "Mission: Impossible - Fallout" into
// "Mission" and "Impossible - Fallout"; the shared run is "Mission: Impossible
// - ", so the prefix is "Mission: Impossible". A title alone in its group, or
// with no separator at all, is left whole.

const SEPARATOR = /\s*:\s+|\s+[-–—]\s+/gu;

export type TitleParts = { prefix: string | null; rest: string };

// separators lists where each separator in s starts and ends.
function separators(s: string): { start: number; end: number }[] {
	const out: { start: number; end: number }[] = [];
	for (const m of s.matchAll(SEPARATOR)) {
		const start = m.index ?? 0;
		// A title cannot start with its prefix's separator.
		if (start === 0) continue;
		out.push({ start, end: start + m[0].length });
	}
	return out;
}

function commonPrefixLength(a: string, b: string): number {
	const n = Math.min(a.length, b.length);
	let i = 0;
	while (i < n && a[i] === b[i]) i++;
	return i;
}

type Entry = {
	title: string;
	seps: { start: number; end: number }[];
	parts: TitleParts;
};

export function splitSeriesPrefixes(titles: readonly string[]): TitleParts[] {
	const entries: Entry[] = titles.map((raw) => {
		const title = raw.trim();
		return { title, seps: separators(title), parts: { prefix: null, rest: title } };
	});

	const groups = new Map<string, Entry[]>();
	for (const e of entries) {
		const first = e.seps[0];
		if (!first) continue;
		const key = e.title.slice(0, first.start);
		const members = groups.get(key);
		if (members) members.push(e);
		else groups.set(key, [e]);
	}

	for (const [head, ...others] of groups.values()) {
		if (!head || others.length === 0) continue;
		let shared = head.title.length;
		for (const e of others) {
			shared = Math.min(shared, commonPrefixLength(head.title, e.title));
		}
		for (const e of [head, ...others]) {
			// The last separator that ends inside the shared run. The first one
			// always does, since it is the group's key; a later one must, too,
			// or two titles that differ just past it would lose what tells them
			// apart.
			const cut = e.seps.filter((sep) => sep.end <= shared).at(-1);
			if (!cut) continue;
			const rest = e.title.slice(cut.end);
			if (!rest) continue;
			e.parts = { prefix: e.title.slice(0, cut.start), rest };
		}
	}
	return entries.map((e) => e.parts);
}
