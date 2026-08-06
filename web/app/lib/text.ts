// Search keys are folded so "amelie" matches "Amélie": NFD splits accented
// characters into base + combining mark, then the marks are dropped.
export function fold(s: string): string {
	return s
		.normalize("NFD")
		.replace(/\p{Diacritic}/gu, "")
		.toLowerCase();
}
