// Search keys are folded so "amelie" matches "Amélie" and "moi quand je me
// reincarne en slime" matches "Moi, quand je me réincarne en Slime": NFD
// splits accented characters into base + combining mark to drop the marks,
// then every run of punctuation or whitespace collapses to a single space.
//
// Punctuation becomes a space rather than vanishing: a separator the user typed
// as a space has to line up with one the title spells with a hyphen, so "spider
// man" finds "Spider-Man" while "spiderman" — which is not how either is
// written — does not.
export function fold(s: string): string {
	return s
		.normalize("NFD")
		.replace(/\p{Diacritic}/gu, "")
		.toLowerCase()
		.replace(/[^\p{L}\p{N}]+/gu, " ")
		.trim();
}
