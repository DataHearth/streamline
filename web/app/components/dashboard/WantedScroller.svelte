<script lang="ts">
	import RecentScroller from "./RecentScroller.svelte";
	import type { ScrollerItem } from "./RecentScroller.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let { movies }: { movies: ScrollerItem[] } = $props();

	let countText = $derived(
		movies.length === 0
			? undefined
			: `${movies.length} title${movies.length === 1 ? "" : "s"} · search nightly`,
	);
</script>

<RecentScroller
	title={i18n.status_wanted()}
	{movies}
	{countText}
	emptyText={i18n.dash_nothing_wanted()}
	pillVariant="solid"
/>
