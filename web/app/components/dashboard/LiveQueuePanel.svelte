<script lang="ts">
	import { onMount, onDestroy } from "svelte";
	import { Activity } from "@lucide/svelte";
	import ProgressBar from "../shared/ProgressBar.svelte";
	import StatusPill from "../shared/StatusPill.svelte";
	import type { QueueItem } from "../../lib/types";

	let { queue }: { queue: QueueItem[] } = $props();

	let reducedMotion = $state(false);
	let tick = $state(0);
	let interval: ReturnType<typeof setInterval> | undefined;

	onMount(() => {
		const mq = window.matchMedia("(prefers-reduced-motion: reduce)");
		reducedMotion = mq.matches;
		if (!reducedMotion) {
			interval = setInterval(() => {
				tick = (tick + 1) % 10_000;
			}, 1200);
		}
	});

	onDestroy(() => {
		if (interval) clearInterval(interval);
	});

	function jitter(base: number, seed: number): number {
		if (reducedMotion) return Math.max(0, Math.min(1, base));
		const noise = Math.sin(tick * 0.7 + seed * 10) * 0.012 + 0.006;
		return Math.max(0, Math.min(0.98, base + noise));
	}

	function releaseTail(release: string | undefined): string {
		if (!release) return "";
		const parts = release.split(".");
		return parts.length > 1 ? parts.slice(-2)[0] : release;
	}

	let downloading = $derived(queue.filter((q) => q.status === "downloading"));
	let grabbing = $derived(queue.filter((q) => q.status === "grabbing"));
	let active = $derived(downloading.length);
</script>

<section
	class="overflow-hidden rounded-lg border border-border bg-bg-elevated"
	aria-label="Live download queue"
>
	<header
		class="flex items-center justify-between border-b border-border px-5 py-4"
	>
		<div class="flex items-center gap-2.5">
			<span
				aria-hidden="true"
				class="inline-block h-1.5 w-1.5 rounded-full bg-status-downloading motion-safe:animate-pulse"
			></span>
			<h3 class="text-sm font-semibold text-fg">Live queue</h3>
			<span class="font-mono text-[11px] text-fg-subtle">
				{active} active
			</span>
		</div>
		<a
			href="/activity"
			class="text-[11.5px] text-fg-subtle transition hover:text-accent-text"
		>
			Open Activity →
		</a>
	</header>

	{#if queue.length === 0}
		<div
			class="flex flex-col items-center justify-center gap-1.5 px-5 py-8 text-center"
		>
			<Activity size={22} class="text-fg-faint" aria-hidden="true" />
			<p class="text-sm font-medium text-fg">Queue is quiet</p>
			<p class="text-xs text-fg-muted">
				Grabs in flight will show up here in real time.
			</p>
		</div>
	{:else}
		<ul class="flex flex-col gap-0.5 p-2">
			{#each downloading as q, i (q.id)}
				<li>
					<a
						href="/movies/{q.movie_id}"
						class="grid grid-cols-[1fr_1fr_auto] items-center gap-4 rounded-md px-3 py-3 transition hover:bg-surface"
					>
						<div class="min-w-0">
							<div class="truncate text-[13px] font-medium text-fg">
								{q.title}
							</div>
							{#if q.release}
								<div class="mt-0.5 truncate font-mono text-[10.5px] text-fg-subtle">
									{releaseTail(q.release)}
								</div>
							{/if}
						</div>
						<div class="min-w-0">
							<ProgressBar
								value={jitter(q.progress, i)}
								status="downloading"
								height={4}
								shimmer
							/>
						</div>
						<div
							class="flex items-center gap-3 whitespace-nowrap font-mono text-[11px] tabular text-fg-muted"
						>
							<span class="font-medium text-fg">
								{Math.round(jitter(q.progress, i) * 100)}%
							</span>
							{#if q.speed}
								<span class="font-medium text-status-downloading">
									↓ {q.speed}<span class="ml-0.5 text-fg-faint">MB/s</span>
								</span>
							{/if}
							{#if q.eta}
								<span>{q.eta}</span>
							{/if}
						</div>
					</a>
				</li>
			{/each}

			{#each grabbing as q (q.id)}
				<li>
					<a
						href="/movies/{q.movie_id}"
						class="grid grid-cols-[1fr_1fr_auto] items-center gap-4 rounded-md px-3 py-3 transition hover:bg-surface"
					>
						<div class="min-w-0">
							<div class="truncate text-[13px] font-medium text-fg">
								{q.title}
							</div>
							<div class="mt-0.5 truncate font-mono text-[10.5px] text-fg-subtle">
								queueing release…
							</div>
						</div>
						<div class="min-w-0">
							<ProgressBar value={1} status="grabbing" height={4} shimmer />
						</div>
						<div
							class="flex items-center gap-3 whitespace-nowrap font-mono text-[11px] tabular text-fg-muted"
						>
							<StatusPill status="grabbing" size="sm" live variant="translucent" />
							{#if q.indexer}
								<span>{q.indexer}</span>
							{/if}
							{#if q.size}
								<span>{q.size}</span>
							{/if}
						</div>
					</a>
				</li>
			{/each}
		</ul>
	{/if}
</section>
