// The four add/request flows AppShell mounts, opened by a window event so a
// caller needn't know whether the modal or the phone screen answers. The
// global search sends what was typed, and the flow opens already searching it;
// every other opener (the + button, a library's empty state) sends nothing and
// the flow opens empty.
export type AddKind = "movie" | "series" | "artist" | "book";

export function openAdd(kind: AddKind, query = "") {
	window.dispatchEvent(new CustomEvent(`streamline:open-add-${kind}`, { detail: { query } }));
}

// The query an open-add event carries, "" when it carries none.
export function addSeed(e: Event): string {
	return (e as CustomEvent<{ query?: string } | null>).detail?.query ?? "";
}
