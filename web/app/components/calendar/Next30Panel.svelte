<script lang="ts">
	import UpcomingList from "../shared/UpcomingList.svelte";
	import type { UpcomingMovie } from "../../lib/types";

	let { events }: { events: UpcomingMovie[] } = $props();
</script>

<UpcomingList {events} title="Next 30 days" />
