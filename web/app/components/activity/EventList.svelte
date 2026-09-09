<script lang="ts">
	import { Activity } from "@lucide/svelte";
	import { formatRelative, formatDateTime } from "../../lib/dates";
	import {
		eventSubject,
		EVENT_MARKS,
		monitoringDetail,
	} from "../../lib/activity-event";
	import type { ActivityEvent } from "../../lib/types";

	let { events }: { events: ActivityEvent[] } = $props();



	function release(payload: Record<string, unknown> | undefined): string {
		if (!payload) return "";
		const v = payload.release_title;
		return typeof v === "string" ? v : "";
	}

	function size(payload: Record<string, unknown> | undefined): string {
		if (!payload) return "";
		const v = payload.size;
		return typeof v === "string" ? v : "";
	}

</script>

<ul class="flex flex-col gap-0.5 p-2">
	{#each events as event (event.id)}
		{@const mark = EVENT_MARKS[event.type] ?? {
			icon: Activity,
			bg: "bg-surface-2",
			fg: "text-fg-muted",
			label: event.type,
		}}
		{@const subject = eventSubject(event)}
		<li>
			<svelte:element
				this={subject.href ? "a" : "div"}
				href={subject.href}
				class="grid grid-cols-[26px_1fr_auto] items-start gap-2.5 rounded-md px-2 py-2.5 transition {subject.href
					? 'hover:bg-surface'
					: ''}"
			>
				<span
					class={`grid h-[22px] w-[22px] place-items-center rounded-sm ${mark.bg} ${mark.fg}`}
				>
					<mark.icon size={13} aria-hidden="true" />
				</span>
				<div class="min-w-0">
					<div class="flex items-baseline justify-between gap-2">
						<span class="truncate text-[12.5px] font-medium text-fg">
							{subject.title}
							{#if subject.detail}
								<span
									class="ml-1 font-mono text-[10.5px] font-normal text-fg-subtle"
									>· {subject.detail}</span
								>
							{/if}
						</span>
						<time
							datetime={event.created_at}
							title={formatDateTime(event.created_at)}
							class="shrink-0 font-mono text-[10.5px] text-fg-faint"
						>
							{formatRelative(event.created_at)}
						</time>
					</div>
					<div class="mt-0.5 truncate font-mono text-[10.5px] text-fg-subtle">
						{release(event.payload) || monitoringDetail(event) || mark.label}
					</div>
				</div>
				{#if size(event.payload)}
					<span class="self-center font-mono text-[10.5px] text-fg-subtle">
						{size(event.payload)}
					</span>
				{/if}
			</svelte:element>
		</li>
	{/each}
</ul>
