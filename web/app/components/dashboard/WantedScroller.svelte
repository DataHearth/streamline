<script lang="ts">
	import RecentScroller from "./RecentScroller.svelte";
	import type { Movie } from "../../lib/types";

	let { movies }: { movies: Movie[] } = $props();

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
