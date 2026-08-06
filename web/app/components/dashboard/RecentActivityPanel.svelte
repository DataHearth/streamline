<script lang="ts">
	import {
		Activity,
		Check,
		Download,
		X,
		GitBranch,
		ShieldCheck,
	} from "@lucide/svelte";
	import { formatRelative, formatDateTime } from "../../lib/dates";
	import type { ActivityEvent, ActivityType } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let { events }: { events: ActivityEvent[] } = $props();

	type Mark = {
		icon: typeof Check;
		bg: string;
		fg: string;
		label: string;
	};

	const MARKS: Record<ActivityType, Mark> = {
		imported: {
			icon: Check,
			bg: "bg-status-available/15",
			fg: "text-status-available",
			label: i18n.activity_imported(),
		},
		download_completed: {
			icon: Check,
			bg: "bg-status-available/15",
			fg: "text-status-available",
			label: i18n.dash_evt_download_completed(),
		},
		grabbed: {
			icon: Download,
			bg: "bg-status-grabbing/15",
			fg: "text-status-grabbing",
			label: i18n.dash_evt_grabbed(),
		},
		download_failed: {
			icon: X,
			bg: "bg-status-failed/15",
			fg: "text-status-failed",
			label: i18n.dash_evt_download_failed(),
		},
		import_failed: {
			icon: X,
			bg: "bg-status-failed/15",
			fg: "text-status-failed",
			label: i18n.dash_evt_import_failed(),
		},
		drift_detected: {
			icon: GitBranch,
			bg: "bg-status-wanted/15",
			fg: "text-status-wanted",
			label: i18n.dash_evt_drift_detected(),
		},
		drift_confirmed: {
			icon: ShieldCheck,
			bg: "bg-status-wanted/15",
			fg: "text-status-wanted",
			label: i18n.dash_evt_drift_confirmed(),
		},
	};

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

<section
	class="overflow-hidden rounded-lg border border-border bg-bg-elevated"
	aria-label={i18n.common_recent_activity()}
>
	<header class="flex items-center justify-between border-b border-border px-5 py-4">
		<h3 class="text-sm font-semibold text-fg">{i18n.common_recent_activity()}</h3>
		<a
			href="/activity"
			class="text-[11.5px] text-fg-subtle transition hover:text-accent-text"
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
		<ul class="flex flex-col gap-0.5 p-2">
			{#each events as event (event.id)}
				{@const mark = MARKS[event.type] ?? {
					icon: Activity,
					bg: "bg-surface-2",
					fg: "text-fg-muted",
					label: event.type,
				}}
				<li>
					<a
						href="/movies/{event.movie.id}"
						class="grid grid-cols-[26px_1fr_auto] items-start gap-2.5 rounded-md px-2 py-2.5 transition hover:bg-surface"
					>
						<span
							class={`grid h-[22px] w-[22px] place-items-center rounded-sm ${mark.bg} ${mark.fg}`}
						>
							<mark.icon size={13} aria-hidden="true" />
						</span>
						<div class="min-w-0">
							<div class="flex items-baseline justify-between gap-2">
								<span class="truncate text-[12.5px] font-medium text-fg">
									{event.movie.title}
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
								{release(event.payload) || mark.label}
							</div>
						</div>
						{#if size(event.payload)}
							<span class="self-center font-mono text-[10.5px] text-fg-subtle">
								{size(event.payload)}
							</span>
						{/if}
					</a>
				</li>
			{/each}
		</ul>
	{/if}
</section>
