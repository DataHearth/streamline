<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import { fly, fade } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { Check, LoaderCircle, Search, X } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { cn } from "@lib/cn";
	import { lockScroll, unlockScroll } from "@lib/scrollLock";
	import { sheetSwipe } from "@lib/sheet-swipe";
	import { IMPORT_KIND } from "@lib/imports";
	import { shortId, type MatchId } from "@lib/imports-touch";
	import { hardcoverIssue, type BookHit } from "@lib/music-books-lookup";
	import { releaseTypeLabel, type ReleaseType } from "@lib/music-books";
	import type {
		ImportScanKind,
		SeriesLookupResultList,
		TMDBMovieResult,
	} from "@lib/types";
	import ProviderKeyNotice from "@components/shared/ProviderKeyNotice.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The match picker below md. Deliberately NOT AddMovieModal/AddSeriesModal —
	// those add to the library; picking here only PATCHes the scan row, so the
	// quality profile, the monitoring preset and the request path are all absent
	// and the confirm verb is "Use", not "Add".
	//
	// Music and books have no add modal with a pick mode to defer to from md up,
	// so `dialog` keeps this one surface at every width: a sheet on a phone, a
	// centred panel from md.
	// `by` and `tag` frame the year on the id line, as the review's candidate
	// rows do: "artist · year · type" for an album, "author · year" for a book.
	// A MusicBrainz release group has no track count and a Hardcover hit no
	// series position, so neither is offered. `sub` is the overview, which only
	// TMDB and TVDB send.
	type Hit = { id: MatchId; title: string; year?: number | null; by?: string; tag?: string; sub?: string };
	type ReleaseHit = { mbid: string; title: string; artist: string; year?: number; type?: ReleaseType };

	let {
		open,
		kind = "movie",
		dialog = false,
		seed = "",
		// The file/folder the pick lands on. Nothing else on this screen says
		// which one it is, and a scan under review usually has several.
		context = "",
		busy = false,
		onClose,
		onPick,
	}: {
		open: boolean;
		kind?: ImportScanKind;
		dialog?: boolean;
		seed?: string;
		context?: string;
		busy?: boolean;
		onClose: () => void;
		onPick: (id: MatchId) => void;
	} = $props();

	let q = $state("");
	let debounced = $state("");
	// Seed with the parsed title alone: the search endpoints take year as a
	// separate param, so folding it into the query text corrupts the search.
	let lastSeed = $state<string | null>(null);
	$effect(() => {
		if (!open) {
			lastSeed = null;
			return;
		}
		if (lastSeed === seed) return;
		lastSeed = seed;
		q = seed;
		debounced = seed;
	});

	let timer: ReturnType<typeof setTimeout> | undefined;
	$effect(() => {
		const raw = q;
		clearTimeout(timer);
		timer = setTimeout(() => (debounced = raw.trim()), 300);
		return () => clearTimeout(timer);
	});

	$effect(() => {
		if (!open) return;
		lockScroll();
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") onClose();
		};
		document.addEventListener("keydown", onKey);
		return () => {
			document.removeEventListener("keydown", onKey);
			unlockScroll();
		};
	});

	const results = createQuery<Hit[]>(() => ({
		queryKey: ["import-match", kind, debounced],
		queryFn: async () => {
			const query = encodeURIComponent(debounced);
			if (kind === "music") {
				const res = await api<{ items: ReleaseHit[] }>(`/music/search/releases?query=${query}`);
				return (res.items ?? []).map((r) => ({
					id: r.mbid,
					title: r.title,
					year: r.year,
					by: r.artist,
					tag: r.type ? releaseTypeLabel(r.type) : undefined,
				}));
			}
			if (kind === "book") {
				const res = await api<{ items: BookHit[] }>(`/books/search?query=${query}&type=book`);
				return (res.items ?? []).map((b) => ({ id: b.hardcover_id, title: b.title, year: b.year, by: b.author }));
			}
			if (kind !== "series") {
				const items = await api<TMDBMovieResult[]>(
					`/search/movie?q=${encodeURIComponent(debounced)}`,
				);
				return items.map((m) => ({
					id: m.tmdb_id,
					title: m.title,
					year: m.year,
					sub: m.overview,
				}));
			}
			const res = await api<SeriesLookupResultList>(
				`/series/lookup?query=${encodeURIComponent(debounced)}`,
			);
			return (res.items ?? []).map((s) => ({
				id: s.tvdb_id,
				title: s.title,
				year: s.year,
				sub: s.overview ?? s.network,
			}));
		},
		enabled: open && debounced.length > 0,
	}));

	let hits = $derived(results.data ?? []);
	let source = $derived(IMPORT_KIND[kind].source);
	// The touch floor only applies below lg, as everywhere else.
	let wide = $state(false);
	$effect(() => {
		const mq = window.matchMedia("(min-width: 768px)");
		const sync = () => (wide = mq.matches);
		sync();
		mq.addEventListener("change", sync);
		return () => mq.removeEventListener("change", sync);
	});
	let centred = $derived(dialog && wide);
</script>

{#if open}
	<div
		class={cn("fixed inset-0 z-50", !dialog && "md:hidden")}
		role="dialog"
		aria-modal="true"
		aria-label={i18n.imports_pick_match()}
	>
		<button
			type="button"
			aria-label={i18n.common_close()}
			transition:fade={{ duration: 160 }}
			onclick={onClose}
			class="absolute inset-0 h-full w-full cursor-default bg-black/55"
		></button>

		<div
			use:sheetSwipe={{ onDismiss: onClose }}
			transition:fly={{ y: centred ? 16 : 420, duration: centred ? 180 : 280, easing: cubicOut }}
			class={cn(
				"absolute inset-x-0 bottom-0 flex max-h-[92dvh] flex-col overflow-hidden rounded-t-2xl border-t border-border-strong bg-bg-elevated shadow-4",
				dialog &&
					"md:inset-x-auto md:bottom-auto md:left-[calc(50%-17.5rem)] md:top-[10dvh] md:max-h-[80dvh] md:w-[35rem] md:rounded-2xl md:border",
			)}
		>
			<div
				class="relative flex cursor-grab touch-none select-none items-center justify-between px-5 pb-3 pt-4 active:cursor-grabbing"
			>
				<span
					aria-hidden="true"
					class={cn("absolute left-1/2 top-2 h-1 w-9 -translate-x-1/2 rounded-full bg-border-strong", dialog && "md:hidden")}
				></span>
				<h2 class="text-[17px] font-semibold tracking-tight text-fg">
					{i18n.imports_pick_match()}
				</h2>
				<button
					type="button"
					onclick={onClose}
					aria-label={i18n.common_close()}
					class="grid h-11 w-11 shrink-0 place-items-center rounded-full bg-surface text-fg-subtle transition active:bg-bg-hover"
				>
					<X size={16} aria-hidden="true" />
				</button>
			</div>

			<div class="px-5 pb-3">
				<div
					class="flex h-11 items-center gap-2.5 rounded-xl border border-border bg-bg-card px-3.5 transition focus-within:border-accent"
				>
					<Search class="h-4 w-4 shrink-0 text-fg-subtle" aria-hidden="true" />
					<input
						type="search"
						bind:value={q}
						placeholder={i18n.imports_search_source({ source })}
						class="min-w-0 flex-1 bg-transparent text-[15px] text-fg outline-none placeholder:text-fg-faint"
					/>
					{#if q}
						<button
							type="button"
							onclick={() => (q = "")}
							aria-label={i18n.common_clear_search()}
							class="grid h-7 w-7 shrink-0 place-items-center rounded-full text-fg-faint transition active:bg-surface"
						>
							<X size={14} aria-hidden="true" />
						</button>
					{/if}
				</div>
			</div>

			{#if context}
				<div
					class="flex items-center gap-2.5 border-y border-accent-line bg-accent-soft px-5 py-2"
				>
					<span
						class="shrink-0 font-mono text-[9.5px] uppercase tracking-[0.14em] text-accent-text"
					>
						{i18n.imports_matching()}
					</span>
					<span class="min-w-0 truncate font-mono text-[11px] text-fg-muted">
						{context}
					</span>
				</div>
			{/if}

			<div
				data-sheet-scroll
				class="min-h-0 flex-1 overflow-y-auto overscroll-contain pb-[max(env(safe-area-inset-bottom),14px)]"
			>
				{#if debounced.length === 0}
					<p class="px-5 py-10 text-center text-sm text-fg-subtle">
						{i18n.imports_type_to_search({ source })}
					</p>
				{:else if results.isPending}
					<p
						class="flex items-center justify-center gap-2 px-5 py-10 text-sm text-fg-subtle"
					>
						<LoaderCircle
							size={15}
							class="motion-safe:animate-spin"
							aria-hidden="true"
						/>
						{i18n.common_searching()}
					</p>
				{:else if results.isError && hardcoverIssue(results.error)}
					<div class="px-5 py-6"><ProviderKeyNotice reason={hardcoverIssue(results.error) ?? undefined} onNavigate={onClose} /></div>
				{:else if results.isError}
					<p class="px-5 py-10 text-center text-sm text-status-failed">
						{i18n.err_load_failed_detail({ reason: errorText(results.error) })}
					</p>
				{:else if hits.length === 0}
					<p class="px-5 py-10 text-center text-sm text-fg-muted">
						{i18n.imports_no_results_for({ source, query: debounced })}
					</p>
				{:else}
					<ul class="divide-y divide-border">
						{#each hits as h (h.id)}
							<li>
								<button
									type="button"
									disabled={busy}
									onclick={() => onPick(h.id)}
									class={cn(
										"flex w-full items-start gap-3 px-5 py-3 text-left transition active:bg-bg-card disabled:opacity-60",
									)}
								>
									<span class="min-w-0 flex-1">
										<span
											class="block truncate text-[14px] font-semibold tracking-[-0.01em] text-fg"
										>
											{h.title}
										</span>
										<span
											class="mt-0.5 block truncate font-mono text-[10.5px] text-fg-subtle"
										>
											{#if h.by}{h.by}{" · "}{/if}{h.year ?? "—"}{#if h.tag}{" · "}{h.tag}{/if} · {source} {shortId(h.id)}
										</span>
										{#if h.sub}
											<span
												class="mt-1 line-clamp-2 block text-[11.5px] leading-relaxed text-fg-faint"
											>
												{h.sub}
											</span>
										{/if}
									</span>
									<span
										class="mt-0.5 inline-flex h-8 shrink-0 items-center gap-1.5 rounded-lg bg-accent px-3 text-[12.5px] font-semibold text-fg-on-accent"
									>
										<Check size={13} aria-hidden="true" />
										{i18n.common_use()}
									</span>
								</button>
							</li>
						{/each}
					</ul>
				{/if}
			</div>
		</div>
	</div>
{/if}
