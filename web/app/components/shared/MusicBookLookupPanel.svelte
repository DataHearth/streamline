<script lang="ts">
	import { BookOpen, Check, ChevronLeft, ExternalLink, Music } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import Img from "./Img.svelte";
	import { lookupPosterUrl } from "@lib/posters";
	import { formatLabel, languageName, releaseTypeLabel } from "@lib/music-books";
	import {
		hitChips,
		volumesCount,
		type ArtistDetail,
		type BookDetail,
		type LookupHit,
		type LookupRelease,
	} from "@lib/music-books-lookup";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// LookupDetailPanel's counterpart for music and books: everything MusicBrainz
	// or Hardcover knows about the highlighted hit. Same props and the same three
	// hosts — the add modal's right pane, the phone sheet's full detent and an
	// expanded request — so each host swaps one panel for the other by kind.
	let {
		kind,
		hit,
		detail,
		loading = false,
		error,
		onBack,
		showTitle = true,
		compact = false,
		headless = false,
		albumPick,
		markedAlbum,
	}: {
		kind: "artist" | "book";
		hit?: LookupHit;
		detail?: ArtistDetail | BookDetail;
		loading?: boolean;
		error?: string;
		onBack?: () => void;
		showTitle?: boolean;
		compact?: boolean;
		headless?: boolean;
		// A request_only member can ask for one album instead of the artist: the
		// discography becomes a pick, and the host's request button follows it.
		albumPick?: { selected: string | null; onToggle: (r: LookupRelease) => void };
		// The album an album request asked for, marked on the reviewer's panel.
		markedAlbum?: string;
	} = $props();

	let isArtist = $derived(kind === "artist");
	let artistDetail = $derived(isArtist ? (detail as ArtistDetail | undefined) : undefined);
	let bookDetail = $derived(!isArtist ? (detail as BookDetail | undefined) : undefined);
	let chips = $derived(hit ? hitChips(hit) : []);
	let about = $derived(detail?.overview ?? "");
	let releases = $derived((artistDetail?.releases ?? []).slice(0, 8));

	// One line per language: which formats exist in it, who publishes it. What
	// a French reader needs to know before adding is whether there is a French
	// ebook or audiobook at all.
	let editionRows = $derived.by(() => {
		const rows = new Map<string, { language: string; original: boolean; formats: Set<string>; publisher: string; year: number }>();
		for (const e of bookDetail?.editions ?? []) {
			const row = rows.get(e.language) ?? { language: e.language, original: false, formats: new Set<string>(), publisher: e.publisher, year: e.year };
			row.formats.add(e.format);
			row.original ||= !!e.original;
			row.year = Math.min(row.year, e.year);
			rows.set(e.language, row);
		}
		return [...rows.values()].map((r) => ({
			...r,
			formats: (["ebook", "audiobook"] as const).filter((f) => r.formats.has(f)).map(formatLabel),
		}));
	});

	type Fact = { label: string; value: string; href?: string };
	let facts = $derived.by<Fact[]>(() => {
		if (!hit) return [];
		if (hit.artist) {
			const a = hit.artist;
			const rows: (Fact | null)[] = [
				a.type ? { label: i18n.common_type(), value: a.type === "group" ? i18n.lookup_group() : i18n.common_person() } : null,
				a.area ? { label: i18n.music_fact_from(), value: a.area } : null,
				a.since ? { label: i18n.music_fact_since(), value: String(a.since) } : null,
				artistDetail?.members?.length ? { label: i18n.lookup_members(), value: artistDetail.members.join(", ") } : null,
				{ label: "MusicBrainz", value: a.mbid.slice(0, 8), href: `https://musicbrainz.org/artist/${a.mbid}` },
			];
			return rows.filter((f) => f !== null);
		}
		const b = hit.book;
		if (!b) return [];
		const rows: (Fact | null)[] = [
			{ label: i18n.books_author(), value: b.author },
			b.year ? { label: i18n.lookup_first_published(), value: String(b.year) } : null,
			bookDetail?.pages ? { label: i18n.lookup_pages(), value: String(bookDetail.pages) } : null,
			b.volumes ? { label: i18n.books_volumes(), value: String(b.volumes) } : null,
			b.type === "series" ? { label: i18n.common_status(), value: b.ongoing ? i18n.books_ongoing() : i18n.books_completed() } : null,
			{ label: "Hardcover", value: String(b.hardcover_id), href: `https://hardcover.app/${b.type === "series" ? "series" : "books"}/${b.hardcover_id}` },
		];
		return rows.filter((f) => f !== null);
	});

	const h4 = "mb-2.5 font-mono text-[10.5px] uppercase tracking-[0.14em] text-fg-faint";
	const chip = "inline-flex h-6 items-center rounded-full border border-border bg-surface px-2.5 font-mono text-[11px] text-fg-muted";
</script>

{#if !hit}
	<div class="flex h-full flex-col items-center justify-center px-8 py-16 text-center">
		{#if isArtist}
			<Music class="mb-3 h-8 w-8 text-fg-faint" aria-hidden="true" />
		{:else}
			<BookOpen class="mb-3 h-8 w-8 text-fg-faint" aria-hidden="true" />
		{/if}
		<p class="text-sm font-medium text-fg-muted">{i18n.lookup_nothing_selected()}</p>
		<p class="mt-1 text-xs text-fg-faint">
			{isArtist ? i18n.lookup_pick_artist() : i18n.lookup_pick_book()}
		</p>
	</div>
{:else}
	<div class="flex flex-col {compact ? 'gap-4' : 'gap-5 px-5 py-5'}">
		{#if onBack}
			<button
				type="button"
				onclick={onBack}
				class="-ml-2 -mt-1 inline-flex w-fit items-center gap-1 rounded-md px-2 py-1 text-xs font-medium text-fg-muted transition hover:bg-surface hover:text-fg md:hidden"
			>
				<ChevronLeft size={14} aria-hidden="true" />
				{i18n.common_results()}
			</button>
		{/if}

		{#if !headless}
			<div class="flex gap-4">
				<div
					class="relative w-[104px] flex-none overflow-hidden border border-white/[0.06] bg-bg-card shadow-2 {isArtist
						? 'aspect-square rounded-full'
						: 'aspect-[2/3] rounded-md'}"
				>
					<div class="absolute inset-0 grid place-items-center text-fg-faint">
						{#if isArtist}
							<Music class="h-7 w-7" aria-hidden="true" />
						{:else}
							<BookOpen class="h-7 w-7" aria-hidden="true" />
						{/if}
					</div>
					{#if hit.image}
						<!-- Never loading="lazy": this panel lives in a portaled modal, and
						     Chrome never fires the deferred load for it. -->
						<Img src={hit.image} alt="" class="relative h-full w-full object-cover" />
					{/if}
				</div>
				<div class="flex min-w-0 flex-1 flex-col justify-center">
					{#if showTitle}
						<h3 class="text-xl font-bold leading-tight tracking-tight text-fg [text-wrap:pretty]">
							{hit.title}
						</h3>
					{/if}
					{#if hit.book}
						<p class="mt-1 truncate text-[13px] text-fg-muted">{i18n.lookup_by({ name: hit.book.author })}</p>
					{/if}
					{#if hit.aside}
						<p class="mt-1 truncate text-[12.5px] italic text-fg-faint">{hit.aside}</p>
					{/if}
					<div class="mt-3 flex flex-wrap gap-1.5">
						{#each chips as c (c)}
							<span class={chip}>{c}</span>
						{/each}
						{#if hit.book?.type === "series" && hit.book.ongoing}
							<span class="{chip} border-status-downloading/30 text-status-downloading">{i18n.books_ongoing()}</span>
						{/if}
						{#if loading}
							<span class="{chip} text-fg-faint">{i18n.lc_loading()}</span>
						{/if}
					</div>
				</div>
			</div>

			{#if about}
				<section>
					<h4 class={h4}>{isArtist ? i18n.lookup_about() : i18n.detail_synopsis()}</h4>
					<p class="text-[13px] leading-relaxed text-fg-muted [text-wrap:pretty]">{about}</p>
				</section>
			{/if}
		{/if}

		{#if error}
			<p
				role="alert"
				class="rounded-md border border-dashed border-status-failed/40 bg-status-failed/5 px-3 py-2.5 text-xs text-status-failed"
			>
				{error}
			</p>
		{:else if loading && !detail}
			<section>
				<h4 class={h4}>{isArtist ? i18n.music_discography() : i18n.books_editions()}</h4>
				<div class="grid grid-cols-4 gap-3">
					{#each [0, 1, 2, 3] as i (i)}
						<div>
							<div class="animate-pulse rounded-md bg-bg-card {isArtist ? 'aspect-square' : 'aspect-[2/3]'}"></div>
							<div class="mt-2 h-2 w-3/4 animate-pulse rounded bg-bg-card"></div>
						</div>
					{/each}
				</div>
			</section>
		{:else if isArtist && releases.length > 0}
			<section>
				<h4 class={h4}>{i18n.music_discography()}</h4>
				{#if albumPick}
					<p class="-mt-1 mb-2.5 text-[12px] text-fg-muted">{i18n.lookup_pick_album_hint()}</p>
				{/if}
				<ul class="grid grid-cols-3 gap-3 sm:grid-cols-4">
					{#each releases as r (r.mbid)}
						{@const on = albumPick ? albumPick.selected === r.mbid : markedAlbum === r.mbid}
						<li class="min-w-0">
							{#if albumPick && r.mbid}
								<button
									type="button"
									aria-pressed={on}
									onclick={() => albumPick.onToggle(r)}
									class="block w-full rounded-md text-left transition focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring active:opacity-80"
								>
									{@render tile(r, on)}
								</button>
							{:else}
								{@render tile(r, on)}
							{/if}
						</li>
					{/each}
				</ul>
			</section>
		{:else if !isArtist && hit.series && (bookDetail?.volume_book_ids?.length ?? 0) > 0}
			<section>
				<h4 class={h4}>{hit.book?.volumes ? volumesCount(hit.book.volumes) : i18n.books_volumes()}</h4>
				<ul class="grid grid-cols-4 gap-2.5 sm:grid-cols-6">
					{#each bookDetail?.volume_book_ids ?? [] as volumeId, i (volumeId)}
						<li class="min-w-0">
							<div class="aspect-[2/3] overflow-hidden rounded-[5px] border border-white/[0.06] bg-bg-card">
								<Img src={lookupPosterUrl("books", volumeId)} alt="" class="h-full w-full object-cover" />
							</div>
							<p class="mt-1 text-center font-mono text-[10px] text-fg-subtle">{i18n.books_volume_n({ n: String(i + 1) })}</p>
						</li>
					{/each}
				</ul>
			</section>
		{:else if !isArtist && editionRows.length > 0}
			<section>
				<h4 class={h4}>{i18n.books_editions()}</h4>
				<ul class="divide-y divide-border overflow-hidden rounded-lg border border-border">
					{#each editionRows as e (e.language)}
						<li class="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-0.5 px-3 py-2.5">
							<span class="text-[13px] font-medium text-fg">
								{languageName(e.language, true)}
								{#if e.original}
									<span class="ml-1 font-mono text-[10px] uppercase tracking-[0.1em] text-fg-faint">{i18n.books_original()}</span>
								{/if}
							</span>
							<span class="text-[12px] text-fg-muted">{e.formats.join(" · ")}</span>
							<span class="w-full truncate font-mono text-[10.5px] text-fg-subtle">{e.publisher} · {e.year}</span>
						</li>
					{/each}
				</ul>
			</section>
		{/if}

		{#if facts.length > 0}
			<section>
				<h4 class={h4}>{i18n.common_details()}</h4>
				<dl class="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-[12px] sm:grid-cols-[auto_1fr_auto_1fr] sm:gap-x-5">
					{#each facts as f (f.label)}
						<dt class="text-fg-subtle">{f.label}</dt>
						<dd class="m-0 min-w-0 truncate text-right font-mono text-fg">
							{#if f.href}
								<a
									href={f.href}
									target="_blank"
									rel="noopener noreferrer"
									class="inline-flex items-center gap-1 text-accent-text transition hover:text-accent"
								>
									{f.value}
									<ExternalLink size={11} aria-hidden="true" />
								</a>
							{:else}
								{f.value}
							{/if}
						</dd>
					{/each}
				</dl>
			</section>
		{/if}
	</div>
{/if}

{#snippet tile(r: LookupRelease, on: boolean)}
	<div
		class={cn(
			"relative aspect-square overflow-hidden rounded-md border bg-bg-card",
			on ? "border-accent ring-2 ring-accent" : "border-white/[0.06]",
		)}
	>
		<Img src={lookupPosterUrl("albums", r.mbid)} alt="" class="h-full w-full object-cover" />
		{#if on}
			<span class="absolute right-1.5 top-1.5 grid h-5 w-5 place-items-center rounded-full bg-accent text-fg-on-accent" aria-hidden="true">
				<Check size={12} />
			</span>
		{/if}
	</div>
	<p class={cn("mt-1.5 truncate text-[12px] font-medium", on ? "text-accent-text" : "text-fg")}>{r.title}</p>
	<p class="truncate font-mono text-[10.5px] text-fg-subtle">
		{[r.year, releaseTypeLabel(r.type)].filter(Boolean).join(" · ")}
	</p>
{/snippet}
