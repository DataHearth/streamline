<script lang="ts">
	import UpcomingList from "../shared/UpcomingList.svelte";
	import type { CalendarEvent } from "../../lib/calendar";

	let { events }: { events: CalendarEvent[] } = $props();
</script>

<UpcomingList {events} title="Next 30 days" />
