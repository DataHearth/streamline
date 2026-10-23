<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import { Check, CircleAlert, CircleCheck, CircleX, LoaderCircle, X } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { cn } from "@lib/cn";
	import { formatBytes } from "@lib/format";
	import type { DiscographyFiles, ReleaseFiles } from "@lib/music-books";
	import type { PackFiles, SearchResult } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// What a release's torrent holds, read against the scope before anything is
	// grabbed: an album's tracks, a season's or show's episodes, or an artist's
	// waiting albums for a discography pack. One request
	// to the indexer per release, so it only runs once someone asks, and a
	// failure is not retried behind their back.
	let {
		path,
		release,
		kind,
	}: { path: string; release: SearchResult; kind: "tracks" | "episodes" | "discography" } = $props();

	// Every answer, folded to one of two shapes. "missing" is what a grab
	// without replace_existing is for: a track with no file, an episode still
	// wanted and aired (one with no file that has not aired yet is not missing).
	// A pack is two levels: the albums the grab links, each with its missing
	// tracks, and the folders the import will skip.
	type Item = { id: number; label: string; title: string; missing: boolean; in: boolean };
	type PackAlbum = { id: number; title: string; in: boolean; missing: number; covered: number };
	type PanelFile = { path: string; size: number; matched: boolean };
	type Folded =
		| { pack: false; items: Item[]; files: PanelFile[] }
		| { pack: true; albums: PackAlbum[]; skipped: { path: string; known: boolean }[]; files: PanelFile[] };

	const pad = (n: number) => String(n).padStart(2, "0");
	function fold(raw: ReleaseFiles | PackFiles | DiscographyFiles): Folded {
		if ("folders" in raw) {
			return {
				pack: true,
				// What the pack fills first, then by year, oldest first.
				albums: [...raw.albums]
					.sort((a, b) => Number(b.in_release) - Number(a.in_release) || (a.year ?? 0) - (b.year ?? 0))
					.map((a) => {
					const gaps = a.tracks.filter((t) => !t.has_file);
					return {
						id: a.id,
						title: a.year ? `${a.title} · ${a.year}` : a.title,
						in: a.in_release,
						missing: gaps.length,
						covered: gaps.filter((t) => t.in_release).length,
					};
				}),
				skipped: raw.folders.filter((f) => !f.waiting).map((f) => ({ path: f.path, known: !!f.album_id })),
				files: raw.files.map((f) => ({ path: f.path, size: f.size, matched: !!f.album_id })),
			};
		}
		if ("tracks" in raw) {
			const multiDisc = raw.tracks.some((t) => t.disc > 1);
			return {
				pack: false,
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
			pack: false,
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
		queryFn: async () =>
			fold(await api<ReleaseFiles | PackFiles | DiscographyFiles>(path, { method: "POST", body: release })),
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
	let text = $derived(TEXT[kind === "discography" ? "tracks" : kind]);

	let items = $derived(q.data && !q.data.pack ? q.data.items : []);
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
	let packAlbums = $derived(q.data?.pack ? q.data.albums : []);
	let packSkipped = $derived(q.data?.pack ? q.data.skipped : []);
	let packCovered = $derived(packAlbums.filter((a) => a.in).length);
	let packTone = $derived<Tone>(
		packAlbums.length === 0 || packCovered === 0 ? "none" : packCovered === packAlbums.length ? "good" : "part",
	);
	let packSummary = $derived.by(() => {
		const count = packAlbums.length;
		if (count === 0) return i18n.release_files_pack_none();
		if (packCovered === count)
			return (count === 1 ? i18n.release_files_pack_all_one : i18n.release_files_pack_all_other)({ count });
		if (packCovered > 0) return i18n.release_files_pack_some({ covered: packCovered, count });
		return (count === 1 ? i18n.release_files_pack_zero_one : i18n.release_files_pack_zero_other)({ count });
	});

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
	{:else if q.data.pack}
		{@render headline(packTone, packSummary)}
		{#if packAlbums.length > 0}
			<div class="mt-2.5 font-mono text-[9.5px] uppercase tracking-[0.14em] text-fg-faint">
				{i18n.release_files_pack_heading()}
			</div>
			<ul class="mt-1 flex max-h-56 flex-col gap-0.5 overflow-y-auto overscroll-contain">
				{#each packAlbums as a (a.id)}
					<li class="flex items-center gap-2">
						{#if a.in}
							<Check size={13} class="shrink-0 text-status-available" aria-label={i18n.release_files_track_in()} />
						{:else}
							<X size={13} class="shrink-0 text-status-failed" aria-label={i18n.release_files_track_out()} />
						{/if}
						<span class={cn("min-w-0 flex-1 truncate", a.in ? "text-fg" : "text-fg-subtle")}>{a.title}</span>
						<span class="shrink-0 font-mono text-[10.5px] tabular text-fg-faint">
							{a.in
								? a.missing > 0
									? i18n.release_files_pack_tracks({ covered: a.covered, count: a.missing })
									: ""
								: i18n.release_files_pack_absent()}
						</span>
					</li>
				{/each}
			</ul>
		{/if}
		{#if packSkipped.length > 0}
			<div class="mt-2.5 font-mono text-[9.5px] uppercase tracking-[0.14em] text-fg-faint">
				{i18n.release_files_pack_skipped_heading()}
			</div>
			<ul class="mt-1 flex max-h-40 flex-col gap-0.5 overflow-y-auto overscroll-contain">
				{#each packSkipped as f (f.path)}
					<li class="min-w-0 text-fg-subtle">
						<span class="break-all font-mono text-[11px]">{f.path}</span>
						<span class="text-[11px] text-fg-faint">
							· {f.known ? i18n.release_files_pack_skip_done() : i18n.release_files_pack_skip_unknown()}
						</span>
					</li>
				{/each}
			</ul>
		{/if}
		{@render fileList(q.data.files)}
		<p class="mt-2 text-[11px] text-fg-faint">{i18n.release_files_pack_hint()}</p>
	{:else}
		{@render headline(tone, summary)}
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
		{@render fileList(q.data.files)}
		<p class="mt-2 text-[11px] text-fg-faint">{text.hint()}</p>
	{/if}
</div>

{#snippet headline(t: Tone, line: string)}
	<p class={cn("flex items-center gap-2 font-medium", TONE[t])}>
		{#if t === "good"}
			<CircleCheck size={14} class="shrink-0" aria-hidden="true" />
		{:else if t === "part"}
			<CircleAlert size={14} class="shrink-0" aria-hidden="true" />
		{:else}
			<CircleX size={14} class="shrink-0" aria-hidden="true" />
		{/if}
		{line}
	</p>
{/snippet}

{#snippet fileList(files: PanelFile[])}
	<details class="mt-2.5">
		<summary class="cursor-pointer select-none text-[11.5px] text-accent-text hover:text-accent-hover">
			{i18n.release_files_list({ count: files.length })}
		</summary>
		<ul class="mt-1.5 flex max-h-56 flex-col gap-0.5 overflow-y-auto overscroll-contain">
			{#each files as f (f.path)}
				<li class="flex items-baseline gap-3">
					<span class={cn("min-w-0 flex-1 break-all font-mono text-[11px]", f.matched ? "text-fg" : "text-fg-subtle")}>
						{f.path}
					</span>
					<span class="shrink-0 font-mono text-[10.5px] tabular text-fg-faint">{formatBytes(f.size)}</span>
				</li>
			{/each}
		</ul>
	</details>
{/snippet}
