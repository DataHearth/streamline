<script lang="ts">
	import { ArrowDown, Search, UserRound } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import { artistNow, artistPill, artistSegments, artistTally, releasesCount, type Artist } from "@lib/music-books";
	import { artistPosterUrl } from "@lib/posters";
	import Poster from "@components/shared/Poster.svelte";
	import LabelPill from "@components/shared/LabelPill.svelte";
	import MonitorToggle from "@components/shared/MonitorToggle.svelte";
	import LibraryActions from "@components/shared/LibraryActions.svelte";
	import SelectBox from "@components/shared/SelectBox.svelte";
	import { longPress } from "@lib/long-press";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// One artist in the library grid, the way a show is one poster in Series:
	// the releases are on the artist page, which opens on the newest one with
	// something missing. The ring is the season bar bent round the photo: what
	// the artist's monitored and on-disk releases are, by colour (on disk, on
	// its way, refused, wanted); grey only when nothing is followed. The pill
	// shows only while something is pending. The line under the name names the release that
	// matters now: in flight first, as the series card names the episode it is
	// fetching, then a refused import, the newest missing one, the next one out.
	// An artist without a photo gets the server's stand-in, its newest cover;
	// the glyph covers the moment before either arrives.
	let {
		artist: a,
		canEdit = false,
		onSearch,
		onMonitor,
		monitorPending = false,
		selected = false,
		selectionActive = false,
		onSelect,
		onLongPress,
	}: {
		artist: Artist;
		canEdit?: boolean;
		onSearch?: () => void;
		onMonitor?: () => void;
		// The monitor write is in flight.
		monitorPending?: boolean;
		// Selection as on PosterCard: opt-in through onSelect, the box behind
		// hover until something is selected, and a touch long press starts it.
		selected?: boolean;
		selectionActive?: boolean;
		onSelect?: (v: boolean) => void;
		onLongPress?: () => void;
	} = $props();

	// While selecting, the card is a selection target rather than a link.
	// Capture phase: Routify's click scope sits below Svelte's delegated root.
	function onCardClick(e: MouseEvent) {
		if (!onSelect || !selectionActive) return;
		e.preventDefault();
		onSelect(!selected);
	}

	let t = $derived(artistTally(a));
	let now = $derived(artistNow(a));
	let href = $derived(`/music/${a.id}`);
	let monitored = $derived(a.monitor !== "none");

	// The ring lives in a 100-unit box so it scales with the column. Arcs start
	// at twelve o'clock, a hairline apart.
	const R = 47.5;
	const C = 2 * Math.PI * R;
	let arcs = $derived.by(() => {
		const segs = artistSegments(a);
		const total = segs.reduce((n, s) => n + s.count, 0);
		if (!total) return [];
		const parts = segs.map((s) => ({ k: s.token, f: s.count / total }));
		const gap = parts.length === 1 ? 0 : 1.8;
		let at = 0;
		return parts.map((p) => {
			const arc = { k: p.k, dash: Math.max(0, p.f * C - gap), offset: -(at * C + gap / 2) };
			at += p.f;
			return arc;
		});
	});

	// Shared with the artist page's photo (artistPill).
	let pill = $derived(artistPill(a));

	const glass =
		"grid h-10 w-10 place-items-center rounded-full border border-white/10 bg-black/65 text-white backdrop-blur-sm transition hover:bg-black/80 lg:h-7 lg:w-7";
</script>

<div
	class="group relative flex min-w-0 flex-col items-center text-center"
	use:longPress={{ onLongPress: () => onLongPress?.(), disabled: !onLongPress || selectionActive }}
>
	<div class="relative w-[88%] max-w-[120px]">
		<a
			{href}
			aria-label={a.name}
			onclickcapture={onCardClick}
			class={cn(
				"relative block aspect-square rounded-full transition duration-200 group-hover:scale-[1.03] focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring focus-visible:ring-offset-2 focus-visible:ring-offset-bg-deep",
				selected && "ring-2 ring-accent ring-offset-2 ring-offset-bg-deep",
			)}
		>
			<svg class="absolute inset-0 h-full w-full -rotate-90" viewBox="0 0 100 100" aria-hidden="true">
				<circle cx="50" cy="50" r={R} fill="none" stroke="rgb(255 255 255 / 0.08)" stroke-width="2.6" />
				{#each arcs as arc (arc.k)}
					<circle
						cx="50"
						cy="50"
						r={R}
						fill="none"
						stroke="var(--status-{arc.k})"
						stroke-width="2.6"
						stroke-dasharray="{arc.dash} {C}"
						stroke-dashoffset={arc.offset}
					/>
				{/each}
			</svg>
			<div class="absolute inset-[6.5%] overflow-hidden rounded-full bg-bg-card">
				<div class="absolute inset-0 grid place-items-center text-fg-faint">
					<UserRound class="h-2/5 w-2/5" aria-hidden="true" />
				</div>
				<Poster src={artistPosterUrl(a.id)} alt="" class="relative h-full w-full object-cover" />
			</div>
		</a>

		{#if pill}
			<span
				class={cn(
					"pointer-events-none absolute bottom-0 left-1/2 -translate-x-1/2 translate-y-[40%] rounded-full transition",
					canEdit && !selectionActive && "group-hover:opacity-0 group-has-[:focus-visible]:opacity-0",
				)}
			>
				<LabelPill token={pill.token} label={pill.label} live={pill.live} />
			</span>
		{/if}

		<!-- The pill makes way for the actions while the card is hovered or
		     focused, as PosterCard's status does for its checkbox. -->
		{#if onSelect}
			<div
				class={cn(
					"absolute left-0 top-0 z-10 transition",
					selectionActive
						? "opacity-100"
						: "pointer-events-none opacity-0 group-hover:pointer-events-auto group-hover:opacity-100 group-has-[:focus-visible]:pointer-events-auto group-has-[:focus-visible]:opacity-100",
				)}
			>
				<SelectBox
					variant="card"
					checked={selected}
					onChange={(v) => onSelect(v)}
					label={selected ? i18n.a11y_deselect_item({ title: a.name }) : i18n.a11y_select_item({ title: a.name })}
				/>
			</div>
		{/if}

		{#if canEdit && !selectionActive}
			<div
				class="pointer-events-none absolute bottom-0 left-1/2 flex -translate-x-1/2 translate-y-[45%] gap-1 opacity-0 transition duration-200 group-hover:pointer-events-auto group-hover:opacity-100 group-has-[:focus-visible]:pointer-events-auto group-has-[:focus-visible]:opacity-100"
			>
				{#if t.wanted > 0}
					<button
						type="button"
						onclick={onSearch}
						aria-label={i18n.music_search_missing_for({ name: a.name })}
						title={i18n.music_search_missing()}
						class={glass}
					>
						<Search size={14} aria-hidden="true" />
					</button>
				{/if}
				<MonitorToggle {monitored} onToggle={() => onMonitor?.()} pending={monitorPending} variant="overlay" />
				<LibraryActions
					base={`/music/artists/${a.id}`}
					title={a.name}
					media="music"
					queryKey="music"
					backHref="/music"
					hasFiles={a.size > 0}
					profile={a.quality_profile}
					searchLabel={t.wanted > 0 ? i18n.music_search_missing() : undefined}
					{onSearch}
					removeBody={i18n.music_remove_body()}
					filesLabel={i18n.music_delete_files_label()}
					variant="card"
				/>
			</div>
		{/if}
	</div>

	<!-- biome-ignore lint/a11y/useAnchorContent: a pointer-only twin of the named link beside it, out of the tab order -->
	<a
		{href}
		tabindex="-1"
		aria-hidden="true"
		onclickcapture={onCardClick}
		class="mt-4 line-clamp-2 w-full text-[13px] font-semibold leading-snug text-fg transition group-hover:text-accent-text"
	>
		{a.name}
	</a>
	<p class="mt-0.5 w-full truncate text-[11.5px] text-fg-subtle">
		{#if now.downloading}
			<span class="text-status-downloading"
				><ArrowDown size={11} class="-mt-px mr-0.5 inline" aria-hidden="true" />{now.downloading.title}</span
			>
			<span class="font-mono text-[10.5px] text-status-downloading"
				>{now.downloading.status === "paused" ? i18n.status_paused() : `${Math.round(now.downloading.progress ?? 0)}%`}</span
			>
		{:else if now.failed}
			<span class="text-status-failed">{now.failed.title}</span>
		{:else if now.wanted.length}
			<span class="text-fg-muted">{now.wanted[0]?.title}</span
			>{#if now.wanted.length > 1}<span class="ml-1 font-mono text-[10.5px] text-fg-subtle">+{now.wanted.length - 1}</span>{/if}
		{:else if now.upcoming}
			<span class="text-fg-muted">{now.upcoming.title}</span>
		{:else}
			{#if a.genre}{a.genre} <span class="text-fg-faint">·</span>{/if}
			<span class="font-mono text-[10.5px]">{t.released ? `${t.have}/${t.released}` : releasesCount(t.out)}</span>
		{/if}
	</p>
</div>
