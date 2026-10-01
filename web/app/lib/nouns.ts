// Counted nouns for the selection bars, bulk toasts and filter sheets. A
// component that says "3 titles" or "Select titles" takes one of these rather
// than an English noun it would glue an "s" onto — French needs the article
// ("les titres") and its own plural, which no suffix rule reproduces.

import { m as i18n } from "./paraglide/messages.js";

type Counted = (inputs: { count: string }) => string;

export type Noun = {
	// "3 titles" — the number formatted, the form picked by the raw count.
	count: (n: number) => string;
	// The noun as a whole set, article included where the language wants one:
	// "titles" / "les titres". Fills slots like "Select {items}".
	items: string;
};

const counted = (one: Counted, other: Counted) => (n: number) =>
	(n === 1 ? one : other)({ count: n.toLocaleString() });

export const NOUN_TITLE: Noun = {
	count: counted(i18n.noun_title_one, i18n.noun_title_other),
	items: i18n.noun_title_items(),
};

export const NOUN_SERIES: Noun = {
	count: counted(i18n.noun_series_one, i18n.noun_series_other),
	items: i18n.noun_series_items(),
};

export const NOUN_FILE: Noun = {
	count: counted(i18n.noun_file_one, i18n.noun_file_other),
	items: i18n.noun_file_items(),
};

export const NOUN_EPISODE: Noun = {
	count: counted(i18n.noun_episode_one, i18n.noun_episode_other),
	items: i18n.noun_episode_items(),
};
