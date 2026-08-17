<script lang="ts">
	import RecentScroller from "./RecentScroller.svelte";
	import type { ScrollerItem } from "./RecentScroller.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let { movies }: { movies: ScrollerItem[] } = $props();

	let countText = $derived(
		movies.length === 0
			? undefined
			: `${
					movies.length === 1
						? i18n.dash_title_count_one({ count: movies.length })
						: i18n.dash_title_count_other({ count: movies.length })
				} · ${i18n.dash_search_nightly()}`,
	);
</script>

<RecentScroller
	title={i18n.status_wanted()}
	{movies}
	{countText}
	emptyText={i18n.dash_nothing_wanted()}
/>
