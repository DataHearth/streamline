import { activeRoute } from "@roxi/routify";

// Routify mounts the incoming route before window.location catches up, so a page
// reading its own query string at mount sees the *previous* URL — a list page
// reached from a detail page's back link came up with default filters while the
// address bar showed the filtered ones. activeRoute carries the URL the page is
// being rendered for, so the filters come off that instead. Only the first
// emission naming this path applies; later ones are the page's own write-back.
export function onRouteQuery(
	path: string,
	apply: (p: URLSearchParams) => void,
) {
	let done = false;
	return activeRoute.subscribe((r) => {
		if (done || !r) return;
		const [routePath, search] = (r.url ?? "").split("?");
		if (routePath !== path) return;
		done = true;
		apply(new URLSearchParams(search ?? ""));
	});
}
