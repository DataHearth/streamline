import { kindLabel, type BookKind } from "./music-books";
import { m as i18n } from "./paraglide/messages.js";

// Quality-profile defaults, per medium. Music has one default. Video has one
// per kind (movies, series) and books one per kind (novel, BD, comic, manga),
// so a profile reports the kinds it is the default for — never a toggle: a
// kind's default is only ever handed to another profile, not cleared.
export type DefaultKind = { value: string; label: string };

export const videoKinds = (): DefaultKind[] => [
	{ value: "movie", label: i18n.movies_label() },
	{ value: "series", label: i18n.settings_series() },
];

const BOOK_KINDS: BookKind[] = ["novel", "bd", "comic", "manga"];
export const bookKinds = (): DefaultKind[] => BOOK_KINDS.map((k) => ({ value: k, label: kindLabel(k) }));

export const heldLabels = (kinds: DefaultKind[], held: string[]) =>
	kinds.filter((k) => held.includes(k.value)).map((k) => k.label).join(", ");

// The row's one accent badge: "Default" for a single-default medium, else the
// kinds it holds ("Default · Movies, Series"), and "all kinds" once a book
// profile holds all four — four names do not fit beside a name at 390.
export function defaultBadge(kinds: DefaultKind[], held: string[]): string | null {
	if (!held.length) return null;
	if (kinds.length <= 1) return i18n.quality_default_badge();
	if (kinds.length > 2 && held.length === kinds.length) return i18n.quality_default_all();
	return i18n.quality_default_for({ kinds: heldLabels(kinds, held) });
}
