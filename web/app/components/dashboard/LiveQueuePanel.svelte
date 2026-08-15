<script lang="ts">
	import { Activity } from "@lucide/svelte";
	import ProgressBar from "../shared/ProgressBar.svelte";
	import TouchRow from "../activity/TouchRow.svelte";
	import { entryHeading, queueMeta } from "../../lib/activity-touch";
	import { formatEta, formatSpeed, pillStatus } from "../../lib/format";
	import type { QueueEntry } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let { queue }: { queue: QueueEntry[] } = $props();

	// Importing has finished downloading but isn't done, so its bar reads full
	// and takes the in-progress token — the same reading ActivityRow gives it.
	function progressOf(q: QueueEntry): number {
		return q.status === "importing" ? 1 : (q.progress ?? 0);
	}
	// A percentage is only true while bytes are moving: importing sits at 100%
	// without being finished, and a failed grab never had progress. Those get
	// the meta line alone.
	function hasPercent(q: QueueEntry): boolean {
		return q.status === "downloading" || q.status === "paused";
	}

	// The state column is one column wide and holds one word. queueMeta's fuller
	// reading — the download client, held bytes, the failure text — overflowed it
	// and printed over the progress bar; it is also what /activity shows, which is
	// where every row here links. The touch row below keeps the full line: it has
	// a line of its own for it.
	const STATE_WORD: Partial<Record<QueueEntry["status"], string>> = {
		importing: i18n.lc_importing(),
		paused: i18n.status_paused(),
		completed: i18n.status_completed(),
		error: i18n.status_failed(),
		failed: i18n.status_failed(),
	};

	let active = $derived(queue.filter((q) => q.status === "downloading").length);
	// The dashboard shows the head of the queue; Activity owns the whole list.
	let rows = $derived(queue.slice(0, 4));
	let more = $derived(queue.length - rows.length);
</script>

<section
	class="flex h-full flex-col overflow-hidden rounded-lg border border-border bg-bg-elevated"
	aria-label={i18n.dash_live_download_queue()}
>
	<header
		class="flex items-center justify-between border-b border-border px-5 py-4"
	>
		<div class="flex items-center gap-2.5">
			<span
				aria-hidden="true"
				class="inline-block h-1.5 w-1.5 rounded-full bg-status-downloading motion-safe:animate-pulse"
			></span>
			<h3 class="text-sm font-semibold text-fg">{i18n.dash_live_queue()}</h3>
			<span class="font-mono text-[11px] text-fg-subtle">
				{active} active
			</span>
		</div>
		<a
			href="/activity"
			class="text-[11.5px] text-fg-subtle transition hover:text-accent-text"
		>
			{i18n.dash_open_activity()}
		</a>
	</header>

	{#if queue.length === 0}
		<div
			class="flex flex-1 flex-col items-center justify-center gap-1.5 px-5 py-8 text-center"
		>
			<Activity size={22} class="text-fg-faint" aria-hidden="true" />
			<p class="text-sm font-medium text-fg">{i18n.activity_queue_quiet()}</p>
			<p class="text-xs text-fg-muted">
				{i18n.dash_grabs_in_flight()}
			</p>
		</div>
	{:else}
		<!-- Below md the ring carries progress and state together, freeing all
		     three text lines for words. The table-shaped row needs width the
		     phone doesn't have. -->
		<div class="md:hidden">
			{#each rows as q (q.id)}
				<TouchRow
					status={pillStatus(q.status)}
					progress={progressOf(q)}
					title={entryHeading(q)}
					release={q.title}
					meta={queueMeta(q)}
					href="/activity"
				/>
			{/each}
		</div>

		<ul class="hidden flex-col gap-0.5 p-2 md:flex">
			{#each rows as q (q.id)}
				<li>
					<a
						href="/activity"
						class="grid grid-cols-[1fr_168px] items-center gap-4 rounded-md px-3 py-3 transition hover:bg-surface"
					>
						<div class="min-w-0">
							<div class="truncate text-[13px] font-medium text-fg">
								{entryHeading(q)}
							</div>
							<div
								class="mt-0.5 truncate font-mono text-[10.5px] text-fg-subtle"
							>
								{q.title}
							</div>
							<!-- Under the words rather than in a column of its own: the bar
							     belongs to the title above it, and a middle column put a
							     hairline of colour between two blocks of text that read as one
							     unit. It also gets the full text width instead of half of it. -->
							<div class="mt-2">
								<ProgressBar
									value={progressOf(q)}
									status={pillStatus(q.status)}
									height={4}
									shimmer={q.status === "downloading" ||
										q.status === "importing"}
								/>
							</div>
						</div>
						<!-- Fixed width, not `auto`: every row is its own grid, so an
						     auto-sized meta column resolved per row and the bars under the
						     titles beside them came out different lengths — a paused row's
						     one word against "62% ↓ 4.2 MB/s 12m". 168px holds the widest
						     reading; short ones sit right-aligned in it. -->
						<div
							class="flex min-w-0 items-center justify-end gap-3 whitespace-nowrap font-mono text-[11px] tabular text-fg-muted"
						>
							{#if hasPercent(q)}
								<span class="font-medium text-fg">
									{Math.round(progressOf(q) * 100)}%
								</span>
							{/if}
							{#if q.status === "downloading"}
								{@const speed = formatSpeed(q.download_speed)}
								{@const eta = formatEta(q.eta)}
								{#if speed}
									<span class="font-medium text-status-downloading">
										↓ {speed}
									</span>
								{/if}
								{#if eta}
									<span>{eta}</span>
								{/if}
							{:else}
								<span
									class="truncate"
									style:color="var(--status-{pillStatus(q.status)})"
								>
									{STATE_WORD[q.status] ?? q.status}
								</span>
							{/if}
						</div>
					</a>
				</li>
			{/each}
		</ul>

		{#if more > 0}
			<a
				href="/activity"
				class="border-t border-border px-5 py-2.5 text-center font-mono text-[11px] text-fg-subtle transition hover:text-accent-text"
			>
				+{more} more in Activity
			</a>
		{/if}
	{/if}
</section>
