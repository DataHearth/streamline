export type AddKind = "movie" | "series" | "artist" | "book";

// Opens one of the add/request flows AppShell mounts, already searching
// `query` when there is one.
export function openAdd(kind: AddKind, query = "") {
	window.dispatchEvent(new CustomEvent(`streamline:open-add-${kind}`, { detail: { query } }));
}

export function addSeed(e: Event): string {
	return (e as CustomEvent<{ query?: string } | null>).detail?.query ?? "";
}
