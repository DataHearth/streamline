<script lang="ts">
	import { Activity } from "@lucide/svelte";
	import EventList from "../activity/EventList.svelte";
	import type { ActivityEvent } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let { events }: { events: ActivityEvent[] } = $props();
</script>

<section
	class="overflow-hidden rounded-lg border border-border bg-bg-elevated"
	aria-label={i18n.common_recent_activity()}
>
	<header class="flex items-center justify-between border-b border-border px-5 py-4">
		<h3 class="text-sm font-semibold text-fg">{i18n.common_recent_activity()}</h3>
		<a
			href="/activity?view=events"
			class="touch-hit text-[11.5px] text-fg-subtle transition hover:text-accent-text"
		>
			{i18n.dash_view_all()}
		</a>
	</header>

	{#if events.length === 0}
		<div
			class="flex flex-col items-center justify-center gap-1.5 px-5 py-8 text-center"
		>
			<Activity size={22} class="text-fg-faint" aria-hidden="true" />
			<p class="text-sm font-medium text-fg">{i18n.dash_no_events()}</p>
			<p class="text-xs text-fg-muted">
				{i18n.dash_events_help()}
			</p>
		</div>
	{:else}
		<EventList {events} />
	{/if}
</section>
