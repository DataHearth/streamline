<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import { Check, CircleAlert, CircleCheck, CircleX, LoaderCircle, X } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { cn } from "@lib/cn";
	import { formatBytes } from "@lib/format";
	import type { ReleaseFiles, ReleaseTrack } from "@lib/music-books";
	import type { SearchResult } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// What a release's torrent holds, read against the album's tracks before
	// anything is grabbed. One request to the indexer per release, so it only
	// runs once someone asks, and a failure is not retried behind their back.
	let { path, release }: { path: string; release: SearchResult } = $props();

	const q = createQuery<ReleaseFiles>(() => ({
		queryKey: ["release-files", path, release.download_url],
		queryFn: () => api<ReleaseFiles>(path, { method: "POST", body: release }),
		staleTime: 5 * 60_000,
		retry: false,
		refetchOnWindowFocus: false,
	}));

	let tracks = $derived(q.data?.tracks ?? []);
	let missing = $derived(tracks.filter((t) => !t.has_file));
	let covered = $derived(missing.filter((t) => t.in_release).length);
	let inRelease = $derived(tracks.filter((t) => t.in_release).length);
	let multiDisc = $derived(tracks.some((t) => t.disc > 1));
	const num = (t: ReleaseTrack) => (multiDisc ? `${t.disc}·` : "") + String(t.number).padStart(2, "0");

	type Tone = "good" | "part" | "none";
	let tone = $derived<Tone>(
		missing.length === 0
			? inRelease === tracks.length
				? "good"
				: "part"
			: covered === missing.length
				? "good"
				: covered > 0
					? "part"
					: "none",
	);
	let summary = $derived.by(() => {
		const count = missing.length;
		if (count === 0) return i18n.release_files_tracks({ covered: inRelease, count: tracks.length });
		if (covered === count)
			return (count === 1 ? i18n.release_files_missing_all_one : i18n.release_files_missing_all_other)({ count });
		if (covered > 0) return i18n.release_files_missing_some({ covered, count });
		return (count === 1 ? i18n.release_files_missing_none_one : i18n.release_files_missing_none_other)({ count });
	});
	const TONE: Record<Tone, string> = {
		good: "text-status-available",
		part: "text-status-wanted",
		none: "text-status-failed",
	};
</script>

<div class="rounded-md border border-border bg-bg-base px-3 py-2.5 text-[12px]">
	{#if q.isPending}
		<p class="flex items-center gap-2 text-fg-subtle">
			<LoaderCircle size={13} class="animate-spin" aria-hidden="true" />
			{i18n.release_files_loading()}
		</p>
	{:else if q.isError}
		<p class="flex items-start gap-2 text-fg-muted">
			<CircleAlert size={13} class="mt-px shrink-0 text-fg-subtle" aria-hidden="true" />
			{errorText(q.error)}
		</p>
	{:else}
		<p class={cn("flex items-center gap-2 font-medium", TONE[tone])}>
			{#if tone === "good"}
				<CircleCheck size={14} class="shrink-0" aria-hidden="true" />
			{:else if tone === "part"}
				<CircleAlert size={14} class="shrink-0" aria-hidden="true" />
			{:else}
				<CircleX size={14} class="shrink-0" aria-hidden="true" />
			{/if}
			{summary}
		</p>
		{#if missing.length > 0}
			<div class="mt-2.5 font-mono text-[9.5px] uppercase tracking-[0.14em] text-fg-faint">
				{i18n.release_files_missing_heading()}
			</div>
			<ul class="mt-1 flex flex-col gap-0.5">
				{#each missing as t (t.id)}
					<li class="flex items-center gap-2">
						{#if t.in_release}
							<Check size={13} class="shrink-0 text-status-available" aria-label={i18n.release_files_track_in()} />
						{:else}
							<X size={13} class="shrink-0 text-status-failed" aria-label={i18n.release_files_track_out()} />
						{/if}
						<span class="shrink-0 font-mono text-[11px] tabular text-fg-faint">{num(t)}</span>
						<span class={cn("min-w-0 truncate", t.in_release ? "text-fg" : "text-fg-subtle")}>{t.title}</span>
					</li>
				{/each}
			</ul>
		{/if}
		<details class="mt-2.5">
			<summary class="cursor-pointer select-none text-[11.5px] text-accent-text hover:text-accent-hover">
				{i18n.release_files_list({ count: q.data.files.length })}
			</summary>
			<ul class="mt-1.5 flex max-h-56 flex-col gap-0.5 overflow-y-auto overscroll-contain">
				{#each q.data.files as f (f.path)}
					<li class="flex items-baseline gap-3">
						<span class={cn("min-w-0 flex-1 break-all font-mono text-[11px]", f.track_id ? "text-fg" : "text-fg-subtle")}>
							{f.path}
						</span>
						<span class="shrink-0 font-mono text-[10.5px] tabular text-fg-faint">{formatBytes(f.size)}</span>
					</li>
				{/each}
			</ul>
		</details>
		<p class="mt-2 text-[11px] text-fg-faint">{i18n.release_files_hint()}</p>
	{/if}
</div>
