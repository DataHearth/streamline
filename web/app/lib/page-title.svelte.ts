// The top bar names the section a root page belongs to. A page whose heading
// follows its own state (a books shelf, the activity view) claims the bar's
// title here instead; the claim's cleanup only clears its own title, so a
// page mounting before the previous one unmounts keeps what it set.
let current = $state<{ v: string } | null>(null);

export const pageTitle = {
	get value(): string | null {
		return current?.v ?? null;
	},
	claim(v: string): () => void {
		const token = { v };
		current = token;
		return () => {
			if (current === token) current = null;
		};
	},
};
