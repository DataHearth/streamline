<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import { Check, CircleAlert, CircleCheck, CircleX, LoaderCircle, X } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { cn } from "@lib/cn";
	import { formatBytes } from "@lib/format";
	import type { ReleaseFiles } from "@lib/music-books";
	import type { PackFiles, SearchResult } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// What a release's torrent holds, read against the scope before anything is
	// grabbed: an album's tracks, or a season's or show's episodes. One request
	// to the indexer per release, so it only runs once someone asks, and a
	// failure is not retried behind their back.
	let {
		path,
		release,
		kind,
	}: { path: string; release: SearchResult; kind: "tracks" | "episodes" } = $props();

	// Both answers, folded to one shape. "missing" is what a grab without
	// replace_existing is for: a track with no file, an episode still wanted
	// and aired (one with no file that has not aired yet is not missing).
	type Item = { id: number; label: string; title: string; missing: boolean; in: boolean };
	type Folded = { items: Item[]; files: { path: string; size: number; matched: boolean }[] };

	const pad = (n: number) => String(n).padStart(2, "0");
	function fold(raw: ReleaseFiles | PackFiles): Folded {
		if ("tracks" in raw) {
			const multiDisc = raw.tracks.some((t) => t.disc > 1);
			return {
				items: raw.tracks.map((t) => ({
					id: t.id,
					label: (multiDisc ? `${t.disc}·` : "") + pad(t.number),
					title: t.title,
					missing: !t.has_file,
					in: t.in_release,
				})),
				files: raw.files.map((f) => ({ path: f.path, size: f.size, matched: !!f.track_id })),
			};
		}
		return {
			items: raw.episodes.map((e) => ({
				id: e.id,
				label: `S${pad(e.season)}E${pad(e.number)}`,
				title: e.title,
				missing: e.wanted,
				in: e.in_release,
			})),
			files: raw.files.map((f) => ({ path: f.path, size: f.size, matched: !!f.episode_id })),
		};
	}

	const q = createQuery<Folded>(() => ({
		queryKey: ["release-files", path, release.download_url],
		queryFn: async () => fold(await api<ReleaseFiles | PackFiles>(path, { method: "POST", body: release })),
		staleTime: 5 * 60_000,
		retry: false,
		refetchOnWindowFocus: false,
	}));

	const TEXT = {
		tracks: {
			all: i18n.release_files_tracks,
			allOne: i18n.release_files_missing_all_one,
			allOther: i18n.release_files_missing_all_other,
			some: i18n.release_files_missing_some,
			noneOne: i18n.release_files_missing_none_one,
			noneOther: i18n.release_files_missing_none_other,
			heading: i18n.release_files_missing_heading,
			hint: i18n.release_files_hint,
		},
		episodes: {
			all: i18n.release_files_episodes,
			allOne: i18n.release_files_ep_missing_all_one,
			allOther: i18n.release_files_ep_missing_all_other,
			some: i18n.release_files_ep_missing_some,
			noneOne: i18n.release_files_ep_missing_none_one,
			noneOther: i18n.release_files_ep_missing_none_other,
			heading: i18n.release_files_ep_missing_heading,
			hint: i18n.release_files_ep_hint,
		},
	};
	let text = $derived(TEXT[kind]);

	let items = $derived(q.data?.items ?? []);
	let missing = $derived(items.filter((t) => t.missing));
	let covered = $derived(missing.filter((t) => t.in).length);
	let inRelease = $derived(items.filter((t) => t.in).length);

	type Tone = "good" | "part" | "none";
	let tone = $derived<Tone>(
		missing.length === 0
			? inRelease === items.length
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
		if (count === 0) return text.all({ covered: inRelease, count: items.length });
		if (covered === count) return (count === 1 ? text.allOne : text.allOther)({ count });
		if (covered > 0) return text.some({ covered, count });
		return (count === 1 ? text.noneOne : text.noneOther)({ count });
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
				{text.heading()}
			</div>
			<ul class="mt-1 flex max-h-56 flex-col gap-0.5 overflow-y-auto overscroll-contain">
				{#each missing as t (t.id)}
					<li class="flex items-center gap-2">
						{#if t.in}
							<Check size={13} class="shrink-0 text-status-available" aria-label={i18n.release_files_track_in()} />
						{:else}
							<X size={13} class="shrink-0 text-status-failed" aria-label={i18n.release_files_track_out()} />
						{/if}
						<span class="shrink-0 font-mono text-[11px] tabular text-fg-faint">{t.label}</span>
						<span class={cn("min-w-0 truncate", t.in ? "text-fg" : "text-fg-subtle")}>{t.title}</span>
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
						<span class={cn("min-w-0 flex-1 break-all font-mono text-[11px]", f.matched ? "text-fg" : "text-fg-subtle")}>
							{f.path}
						</span>
						<span class="shrink-0 font-mono text-[10.5px] tabular text-fg-faint">{formatBytes(f.size)}</span>
					</li>
				{/each}
			</ul>
		</details>
		<p class="mt-2 text-[11px] text-fg-faint">{text.hint()}</p>
	{/if}
</div>
