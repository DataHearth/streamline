<script lang="ts">
	import { cn } from "@lib/cn";

	let {
		src,
		alt,
		class: klass = "",
		onerror,
		...rest
	}: {
		src: string;
		alt: string;
		class?: string;
		onerror?: (event: Event) => void;
		[key: string]: unknown;
	} = $props();

	let loaded = $state(false);

	async function handleLoad(event: Event) {
		const img = event.currentTarget as HTMLImageElement;
		const decoding = img.src;
		try {
			await img.decode();
		} catch {
			// `decode()` also rejects when `src` changes under it, which is not a
			// failed image — only report a failure while the element still holds
			// what was decoding. Revealing either way is deliberate: a caller with
			// no error path gets the browser's own broken-image behavior instead of
			// an element that stays invisible forever.
			if (img.src === decoding) onerror?.(event);
		}
		loaded = true;
	}
</script>

<!-- The transition comes first and the caller's classes override it: a caller
that already animates the element (`transition duration-300` for a hover zoom)
must keep its own timing, and tailwind-merge resolves the conflict in favour of
whichever lands last. `opacity-0` goes last for the opposite reason — it has to
beat a caller's own opacity (the blurred hero backdrops sit at `opacity-70`),
and it is dropped rather than paired with `opacity-100` so that the revealed
image settles back to whatever the caller asked for. -->
<img
	{src}
	{alt}
	class={cn(
		"transition-opacity duration-200 motion-reduce:transition-none",
		klass,
		!loaded && "opacity-0",
	)}
	onload={handleLoad}
	{onerror}
	{...rest}
/>
