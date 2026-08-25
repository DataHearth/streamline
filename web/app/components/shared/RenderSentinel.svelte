<script lang="ts">
	import { onMount } from "svelte";
	import type { IncrementalList } from "../../lib/incremental-list.svelte";

	let { list }: { list: IncrementalList<unknown> } = $props();

	let el: HTMLElement;

	onMount(() => {
		// Safety net for fast scrolling that outruns the idle pump: growing
		// before the sentinel actually enters the viewport hides the gap.
		const io = new IntersectionObserver(
			(entries) => {
				if (entries.some((e) => e.isIntersecting)) list.grow();
			},
			{ rootMargin: "600px" },
		);
		io.observe(el);
		return () => io.disconnect();
	});

	$effect(() => {
		if (!list.pending) return;
		// requestIdleCallback is missing in Safari; a timer is close enough.
		// The timeout bounds how long a busy (or hidden) page can defer growth.
		const schedule =
			window.requestIdleCallback?.bind(window) ??
			((cb: () => void, _opts?: IdleRequestOptions) =>
				window.setTimeout(cb, 100));
		const unschedule =
			window.cancelIdleCallback?.bind(window) ?? window.clearTimeout.bind(window);
		const handle = schedule(() => list.grow(), { timeout: 300 });
		return () => unschedule(handle);
	});
</script>

<div bind:this={el} class="h-px" aria-hidden="true"></div>
