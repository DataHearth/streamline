export type AddKind = "movie" | "series" | "artist" | "book";

const KINDS: AddKind[] = ["movie", "series", "artist", "book"];
const eventName = (kind: AddKind) => `streamline:open-add-${kind}`;

// Opens one of the add/request flows AppShell mounts, already searching
// `query` when there is one.
export function openAdd(kind: AddKind, query = "") {
	window.dispatchEvent(new CustomEvent(eventName(kind), { detail: { query } }));
}

// AppShell's side of openAdd. `query` is "" for an opener that sends none.
export function onOpenAdd(handler: (kind: AddKind, query: string) => void): () => void {
	const offs = KINDS.map((kind) => {
		const listener = (e: Event) =>
			handler(kind, (e as CustomEvent<{ query?: string } | null>).detail?.query ?? "");
		window.addEventListener(eventName(kind), listener);
		return () => window.removeEventListener(eventName(kind), listener);
	});
	return () => {
		for (const off of offs) off();
	};
}
