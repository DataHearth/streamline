// The top bar names the section a root page belongs to. A page whose heading
// follows its own state (a books shelf, the activity view) claims the bar's
// title here instead; the claim's cleanup only clears its own title, so a
// page mounting before the previous one unmounts keeps what it set.
//
// A claim names the path it is for, and the bar only reads it back on that
// path. A claim made on /books was seen outliving the page and naming
// /calendar "Books" after a client-side navigation, and location.pathname
// cannot stand in for the path: the next page mounts, and claims, before the
// URL changes.
let current = $state<{ path: string; v: string } | null>(null);

export const pageTitle = {
	valueFor(path: string): string | null {
		return current && current.path === path ? current.v : null;
	},
	claim(path: string, v: string): () => void {
		const token = { path, v };
		current = token;
		return () => {
			if (current === token) current = null;
		};
	},
};
