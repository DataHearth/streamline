<script lang="ts">
	import UpcomingList from "../shared/UpcomingList.svelte";
	import type { CalendarEvent } from "../../lib/calendar";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let { events }: { events: CalendarEvent[] } = $props();
</script>

<UpcomingList {events} title={i18n.dash_next_30_days()} />
