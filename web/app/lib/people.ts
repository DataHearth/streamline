// The monogram that stands in for a missing profile photo — the cast grid and
// the person page draw the same one, so a face that TMDB has no picture for
// looks the same wherever it appears.
export function initials(name: string): string {
	return name
		.split(/\s+/)
		.filter(Boolean)
		.map((p) => p[0])
		.join("")
		.slice(0, 2)
		.toUpperCase();
}
