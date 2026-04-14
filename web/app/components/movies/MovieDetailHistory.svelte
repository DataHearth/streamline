<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import {
		Activity,
		Check,
		Download,
		X,
		GitBranch,
		ShieldCheck,
	} from "@lucide/svelte";
	import { api } from "../../lib/api";
	import { formatRelative, formatDateTime } from "../../lib/dates";
	import type {
		ActivityEvent,
		ActivityList,
		ActivityType,
	} from "../../lib/types";

	let { movieId }: { movieId: number } = $props();

	const q = createQuery<ActivityList>(() => ({
		queryKey: ["movie", movieId, "history"],
		queryFn: () =>
			api<ActivityList>(`/activity?movie_id=${movieId}&limit=50`),
	}));

	type Mark = {
		icon: typeof Activity;
		bg: string;
		fg: string;
		label: string;
	};

	const MARKS: Record<ActivityType, Mark> = {
		imported: {
			icon: Check,
			bg: "bg-status-available/15",
			fg: "text-status-available",
			label: "IMPORTED",
		},
		download_completed: {
			icon: Check,
			bg: "bg-status-available/15",
			fg: "text-status-available",
			label: "DOWNLOAD COMPLETED",
		},
		grabbed: {
			icon: Download,
			bg: "bg-status-grabbing/15",
			fg: "text-status-grabbing",
			label: "GRABBED",
		},
		download_failed: {
			icon: X,
			bg: "bg-status-failed/15",
			fg: "text-status-failed",
			label: "DOWNLOAD FAILED",
		},
		import_failed: {
			icon: X,
			bg: "bg-status-failed/15",
			fg: "text-status-failed",
			label: "IMPORT FAILED",
		},
		drift_detected: {
			icon: GitBranch,
			bg: "bg-status-wanted/15",
			fg: "text-status-wanted",
			label: "DRIFT DETECTED",
		},
		drift_confirmed: {
			icon: ShieldCheck,
			bg: "bg-status-wanted/15",
			fg: "text-status-wanted",
			label: "DRIFT CONFIRMED",
		},
	};

	function release(event: ActivityEvent): string {
		const p = event.payload ?? {};
		const v = p.release_title;
		return typeof v === "string" ? v : "";
	}

	function size(event: ActivityEvent): string {
		const p = event.payload ?? {};
		const v = p.size;
		return typeof v === "string" ? v : "";
	}

	let events = $derived(q.data?.events ?? []);
</script>

{#if q.isLoading}
	<ul aria-hidden="true" class="flex flex-col gap-2">
		{#each Array(5) as _}
			<li
				class="h-14 animate-pulse rounded-md bg-bg-card/50 motion-reduce:animate-none"
			></li>
		{/each}
	</ul>
{:else if q.isError}
	<div
		role="alert"
		class="rounded-lg border border-dashed border-status-failed/40 bg-status-failed/5 py-10 text-center text-sm text-status-failed"
	>
		{q.error?.message ?? "Failed to load history"}
	</div>
{:else if events.length === 0}
	<div
		class="rounded-lg border border-dashed border-border bg-bg-elevated/40 py-12 text-center"
	>
		<Activity
			size={28}
			class="mx-auto mb-3 text-fg-faint"
			aria-hidden="true"
		/>
		<p class="text-sm font-medium text-fg">No history yet</p>
		<p class="mt-1 text-xs text-fg-muted">
			Grabs, imports, and drift events will appear here.
		</p>
	</div>
{:else}
	<ul
		class="overflow-hidden rounded-lg border border-border bg-bg-elevated"
		role="list"
	>
		{#each events as event (event.id)}
			{@const mark = MARKS[event.type] ?? {
				icon: Activity,
				bg: "bg-surface-2",
				fg: "text-fg-muted",
				label: event.type.toUpperCase(),
			}}
			{@const rel = release(event)}
			{@const sz = size(event)}
			<li
				class="grid grid-cols-[28px_1fr_auto_auto] items-center gap-4 border-b border-border px-4 py-3 last:border-b-0"
			>
				<span
					class={`grid h-6 w-6 place-items-center rounded-sm ${mark.bg} ${mark.fg}`}
				>
					<mark.icon size={13} aria-hidden="true" />
				</span>
				<div class="min-w-0">
					<div class="font-mono text-[10px] tracking-[0.1em] text-fg-faint">
						{mark.label}
					</div>
					{#if rel}
						<div class="mt-0.5 truncate font-mono text-[11.5px] text-fg-muted">
							{rel}
						</div>
					{/if}
				</div>
				<time
					datetime={event.created_at}
					title={formatDateTime(event.created_at)}
					class="font-mono text-[11px] text-fg-subtle"
				>
					{formatRelative(event.created_at)}
				</time>
				<span class="font-mono text-[11px] text-fg-subtle">
					{sz || ""}
				</span>
			</li>
		{/each}
	</ul>
{/if}
