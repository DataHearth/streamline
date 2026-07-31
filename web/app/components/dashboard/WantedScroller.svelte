<script lang="ts">
	import RecentScroller from "./RecentScroller.svelte";
	import type { ScrollerItem } from "./RecentScroller.svelte";

	let { movies }: { movies: ScrollerItem[] } = $props();

	let countText = $derived(
		movies.length === 0
			? undefined
			: `${movies.length} title${movies.length === 1 ? "" : "s"} · search nightly`,
	);
</script>

<RecentScroller
	title="Wanted"
	{movies}
	{countText}
	emptyText="Nothing wanted right now."
	pillVariant="solid"
/>
