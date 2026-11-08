<script lang="ts">
	import { DOWNLOAD_CHIP, DOWNLOAD_CHIP_ARROW } from "@lib/title-downloads";
	import { Activity, ChevronRight } from "@lucide/svelte";
	import { formatRelative, formatDateTime } from "@lib/dates";
	import {
		eventDownloadHref,
		eventSubject,
		EVENT_MARKS,
		monitoringDetail,
	} from "@lib/activity-event";
	import type { ActivityEvent } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

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
		{@const downloadHref = eventDownloadHref(event)}
		<!-- The row opens the title; the download it came from, when there is
		     one, is its own link beside it (a link cannot hold another). -->
		<li class="flex items-start gap-1">
			<svelte:element
				this={subject.href ? "a" : "div"}
				href={subject.href}
				class="grid min-w-0 flex-1 grid-cols-[26px_minmax(0,1fr)_auto] items-start gap-2.5 rounded-md px-2 py-2.5 transition {subject.href
					? 'hover:bg-surface'
					: ''}"
			>
				<span
					class={`grid h-[22px] w-[22px] place-items-center rounded-sm ${mark.bg} ${mark.fg}`}
				>
					<mark.icon size={13} aria-hidden="true" />
				</span>
				<div class="min-w-0">
					<span class="block truncate text-[12.5px] font-medium text-fg">
						{subject.title}
						{#if subject.detail}
							<span
								class="ml-1 font-mono text-[10.5px] font-normal text-fg-subtle"
								>· {subject.detail}</span
							>
						{/if}
					</span>
					<div class="mt-0.5 truncate font-mono text-[10.5px] text-fg-subtle">
						{release(event.payload) || monitoringDetail(event) || mark.label}
					</div>
				</div>
				<!-- When and how big share one right-hand column, stacked on the row's two
				     lines. The time inside the title block and the size in a track of its
				     own would put the two right edges 80px apart and neither of them on a
				     line the eye can follow down the feed. -->
				<div class="flex flex-col items-end gap-0.5 font-mono text-[10.5px] tabular-nums">
					<time
						datetime={event.created_at}
						title={formatDateTime(event.created_at)}
						class="whitespace-nowrap text-fg-faint"
					>
						{formatRelative(event.created_at)}
					</time>
					<span class="text-fg-subtle">{size(event.payload)}</span>
				</div>
			</svelte:element>
			{#if downloadHref}
				<a
					href={downloadHref}
					title={i18n.detail_open_download()}
					class="mt-2.5 shrink-0 {DOWNLOAD_CHIP}"
				>
					{i18n.detail_download_link()}
					<span class={DOWNLOAD_CHIP_ARROW}><ChevronRight size={12} aria-hidden="true" /></span>
				</a>
			{/if}
		</li>
	{/each}
</ul>
