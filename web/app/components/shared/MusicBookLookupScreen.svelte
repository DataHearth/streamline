<script lang="ts">
	import { tick } from "svelte";
	import { createQuery, createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { ArrowUpRight, BookOpen, Check, ChevronLeft, LoaderCircle, Music, Plus, Search, X } from "@lucide/svelte";
	import { fly } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { api, errorText } from "@lib/api";
	import { toast } from "@lib/toast";
	import { auth } from "@lib/auth.svelte";
	import { lockScroll, unlockScroll } from "@lib/scrollLock";
	import type { QualityProfile } from "@lib/types";
	import {
		addRequest,
		hitChips,
		libraryHref,
		libraryRoot,
		lookupDetail,
		lookupSearch,
		monitorOptions,
		profileMedia,
		requestBody,
		LOOKUP_SOURCE,
		type LookupHit,
		type LookupKind,
	} from "@lib/music-books-lookup";
	import LookupSheet from "./LookupSheet.svelte";
	import MusicBookLookupPanel from "./MusicBookLookupPanel.svelte";
	import Select from "@components/forms/Select.svelte";
	import Img from "./Img.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The touch add/request flow for an artist or a book: MediaLookupScreen's
	// shape — the results as a grid, a sheet that peeks to confirm the pick and
	// swipes up to the full detail. Monitoring stays at its first option here, as
	// the series screen leaves its preset; the md modal is where that choice is
	// worth the extra control.
	type Props = { kind: LookupKind; open: boolean; onClose: () => void };
	let { kind, open, onClose }: Props = $props();

	let isArtist = $derived(kind === "artist");
	let canAdd = $derived(auth.canAddDirectly);
	let media = $derived(profileMedia(kind));

	let query = $state("");
	let debounced = $state("");
	let qualityProfileName = $state("");
	let selectedKey = $state<string | null>(null);
	let sheetExpanded = $state(false);
	let pendingKey = $state<string | null>(null);
	let sessionAdds = $state(new Map<string, number>());
	let requested = $state(new Set<string>());
	let failedImages = $state(new Set<string>());
	let input = $state<HTMLInputElement | null>(null);
	let debounceTimer: ReturnType<typeof setTimeout> | undefined;

	$effect(() => {
		const q = query;
		clearTimeout(debounceTimer);
		debounceTimer = setTimeout(() => (debounced = q.trim()), 300);
		return () => clearTimeout(debounceTimer);
	});

	$effect(() => {
		if (!open) {
			query = "";
			debounced = "";
			qualityProfileName = "";
			selectedKey = null;
			sheetExpanded = false;
			sessionAdds = new Map();
			requested = new Set();
			failedImages = new Set();
			return;
		}
		lockScroll();
		tick().then(() => input?.focus());
		return unlockScroll;
	});

	const qpQuery = createQuery<QualityProfile[]>(() => ({
		queryKey: ["quality-profiles", media],
		queryFn: () => api<QualityProfile[]>(`/quality-profiles?media=${media}`),
		enabled: open,
	}));
	const searchQuery = createQuery<LookupHit[]>(() => ({
		queryKey: ["mb-lookup", kind, debounced],
		queryFn: () => lookupSearch(kind, debounced),
		enabled: open && debounced.length >= 2,
		staleTime: 60_000,
	}));
	let results = $derived(searchQuery.data ?? []);
	let selected = $derived(results.find((r) => r.key === selectedKey));
	const detailQuery = createQuery(() => ({
		queryKey: ["mb-lookup-detail", selected?.key ?? null],
		queryFn: () => lookupDetail(selected!),
		enabled: open && !!selected,
		staleTime: 5 * 60_000,
	}));

	const qc = useQueryClient();
	const addMutation = createMutation<{ id: number } | null, Error, LookupHit>(() => ({
		onMutate: (h) => {
			pendingKey = h.key;
		},
		mutationFn: async (h) => {
			if (!canAdd) {
				await api("/requests", { method: "POST", body: requestBody(h, qualityProfileName) });
				return null;
			}
			const { path, body } = addRequest(h, qualityProfileName, monitorOptions(h)[0]?.value ?? "");
			return api<{ id: number }>(path, { method: "POST", body });
		},
		onSuccess: (item, h) => {
			if (!canAdd || !item) {
				requested = new Set(requested).add(h.key);
				qc.invalidateQueries({ queryKey: ["requests"] });
				toast.ok(i18n.toast_requested({ title: h.title }));
			} else {
				sessionAdds = new Map(sessionAdds).set(h.key, item.id);
				qc.invalidateQueries({ queryKey: [libraryRoot(kind)] });
				toast.ok(i18n.toast_added({ title: h.title }));
			}
			// Back to the grid: the badge carries the new state.
			selectedKey = null;
		},
		onError: (e) => toast.err(errorText(e, i18n.common_add_failed())),
		onSettled: () => {
			pendingKey = null;
		},
	}));

	let qpOptions = $derived([
		{ value: "", label: canAdd ? i18n.quality_server_default() : i18n.quality_no_preference() },
		...(qpQuery.data ?? []).map((p) => ({ value: p.name, label: p.name })),
	]);

	const isHeld = (h: LookupHit) => h.held || sessionAdds.has(h.key);
	let heldCount = $derived(results.filter(isHeld).length);
	let selectedHeld = $derived(selected ? isHeld(selected) : false);
	let selectedLocalId = $derived(selected ? (selected.library_id ?? sessionAdds.get(selected.key)) : undefined);
	let selectedRequested = $derived(selected ? requested.has(selected.key) : false);
	let selectedPending = $derived(selected ? pendingKey === selected.key : false);
	let synopsis = $derived(detailQuery.data?.overview ?? "");
	let addLabel = $derived(
		!canAdd
			? i18n.action_request()
			: isArtist
				? i18n.action_add_artist()
				: selected?.series
					? i18n.action_add_series()
					: i18n.action_add_book(),
	);

	function pick(h: LookupHit) {
		selectedKey = h.key;
		sheetExpanded = false;
	}
	function markImageFailed(key: string) {
		failedImages = new Set(failedImages).add(key);
	}
	const thumb = $derived(isArtist ? "aspect-square rounded-full" : "aspect-[2/3] rounded-lg");
</script>

{#if open}
	<div class="fixed inset-0 z-50 flex flex-col bg-bg-deep md:hidden" transition:fly={{ y: 28, duration: 200, easing: cubicOut }}>
		<header class="flex flex-none items-center justify-between gap-3 px-3 pt-3 pb-2">
			<button
				type="button"
				onclick={onClose}
				class="-ml-1 inline-flex items-center gap-0.5 rounded-md py-1 pr-2 pl-1 text-[15px] text-accent-text transition active:opacity-70"
			>
				<ChevronLeft size={20} aria-hidden="true" />
				{i18n.nav_library()}
			</button>
			<span class="font-mono text-[10.5px] uppercase tracking-[0.16em] text-fg-faint">{LOOKUP_SOURCE[kind]}</span>
		</header>

		<div class="relative flex-none px-4">
			<Search class="pointer-events-none absolute left-7 top-1/2 h-4 w-4 -translate-y-1/2 text-fg-faint" aria-hidden="true" />
			<input
				type="search"
				bind:this={input}
				bind:value={query}
				placeholder={isArtist ? i18n.lookup_search_mb_placeholder() : i18n.lookup_search_hc_placeholder()}
				autocomplete="off"
				aria-label={isArtist ? i18n.lookup_search_mb() : i18n.lookup_search_hc()}
				class="h-11 w-full rounded-xl border border-border bg-bg-card pr-10 pl-10 text-base text-fg outline-none focus:border-accent focus:ring-2 focus:ring-accent-ring placeholder:text-fg-faint"
			/>
			{#if query.length > 0}
				<button
					type="button"
					onclick={() => {
						query = "";
						input?.focus();
					}}
					aria-label={i18n.common_clear_search()}
					class="absolute right-6 top-1/2 grid h-7 w-7 -translate-y-1/2 place-items-center rounded-full bg-surface text-fg-subtle transition active:opacity-70"
				>
					<X size={13} aria-hidden="true" />
				</button>
			{/if}
		</div>

		{#if debounced.length >= 2 && !searchQuery.isLoading && !searchQuery.isError && results.length > 0}
			<p class="flex-none px-5 pt-3 pb-1 text-[11.5px] text-fg-faint">
				{(results.length === 1 ? i18n.lookup_match_count_one : i18n.lookup_match_count_other)({ count: results.length })}{heldCount > 0
					? ` · ${i18n.lookup_held_in_library({ count: heldCount })}`
					: ""}
			</p>
		{/if}

		<div class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-4 pt-2 pb-8">
			{#if debounced.length < 2}
				<div class="flex flex-col items-center justify-center px-8 py-20 text-center">
					<Search class="mb-3 h-8 w-8 text-fg-faint" aria-hidden="true" />
					<p class="text-sm font-medium text-fg-muted">{isArtist ? i18n.lookup_mb_prompt() : i18n.lookup_hc_prompt()}</p>
					<p class="mt-1 text-xs text-fg-faint">{isArtist ? i18n.lookup_type_2_artist() : i18n.lookup_type_2_book()}</p>
				</div>
			{:else if searchQuery.isLoading}
				<div class="grid grid-cols-3 gap-3">
					{#each [0, 1, 2, 3, 4, 5] as i (i)}
						<div>
							<div class="animate-pulse bg-bg-card {thumb}"></div>
							<div class="mt-2 h-2.5 w-3/4 animate-pulse rounded bg-bg-card"></div>
						</div>
					{/each}
				</div>
			{:else if searchQuery.isError}
				<p
					role="alert"
					class="rounded-lg border border-dashed border-status-failed/40 bg-status-failed/5 py-10 text-center text-xs text-status-failed"
				>
					{errorText(searchQuery.error, i18n.common_search_failed())}
				</p>
			{:else if results.length === 0}
				<div class="flex flex-col items-center justify-center px-8 py-20 text-center">
					{#if isArtist}
						<Music class="mb-3 h-8 w-8 text-fg-faint" aria-hidden="true" />
					{:else}
						<BookOpen class="mb-3 h-8 w-8 text-fg-faint" aria-hidden="true" />
					{/if}
					<p class="text-sm font-medium text-fg-muted">{i18n.common_no_matches()}</p>
					<p class="mt-1 text-xs text-fg-faint">
						{(isArtist ? i18n.lookup_nothing_on_mb : i18n.lookup_nothing_on_hc)({ query: debounced })}
					</p>
				</div>
			{:else}
				<ul class="grid grid-cols-3 gap-3">
					{#each results as r (r.key)}
						{@const done = isHeld(r) || requested.has(r.key)}
						<li>
							<button type="button" onclick={() => pick(r)} class="w-full text-left transition active:opacity-80">
								<div class="relative">
									<div class="relative overflow-hidden border border-white/[0.06] bg-bg-card shadow-1 {thumb}">
										<div class="absolute inset-0 grid place-items-center text-fg-faint">
											{#if isArtist}
												<Music class="h-6 w-6" aria-hidden="true" />
											{:else}
												<BookOpen class="h-6 w-6" aria-hidden="true" />
											{/if}
										</div>
										{#if r.image && !failedImages.has(r.key)}
											<Img src={r.image} alt="" onerror={() => markImageFailed(r.key)} class="relative h-full w-full object-cover" />
										{/if}
									</div>
									<span
										aria-hidden="true"
										class="absolute right-1 bottom-1 grid h-7 w-7 place-items-center rounded-full shadow-2 {done
											? 'border border-accent-line bg-black/70 text-accent-text'
											: 'bg-accent text-fg-on-accent'}"
									>
										{#if done}
											<Check size={14} aria-hidden="true" />
										{:else}
											<Plus size={16} aria-hidden="true" />
										{/if}
									</span>
								</div>
								<p class="mt-1.5 truncate text-[12.5px] font-medium text-fg {isArtist ? 'text-center' : ''}">{r.title}</p>
								<p class="mt-0.5 truncate font-mono text-[10.5px] text-fg-subtle {isArtist ? 'text-center' : ''}">
									{isArtist ? (r.artist?.genre ?? "—") : (r.subtitle ?? "—")}
								</p>
							</button>
						</li>
					{/each}
				</ul>
			{/if}
		</div>
	</div>

	<LookupSheet
		open={selected !== undefined}
		bind:expanded={sheetExpanded}
		label={isArtist ? i18n.lookup_artist_details() : i18n.lookup_book_details()}
		onClose={() => (selectedKey = null)}
	>
		{#snippet peek(atFullHeight)}
			{#if selected}
				<div class="flex gap-4 pt-1">
					<div class="relative w-[84px] flex-none overflow-hidden border border-white/[0.06] bg-bg-card shadow-2 {thumb}">
						<div class="absolute inset-0 grid place-items-center text-fg-faint">
							{#if isArtist}
								<Music class="h-6 w-6" aria-hidden="true" />
							{:else}
								<BookOpen class="h-6 w-6" aria-hidden="true" />
							{/if}
						</div>
						{#if selected.image && !failedImages.has(selected.key)}
							<Img src={selected.image} alt="" class="relative h-full w-full object-cover" />
						{/if}
					</div>
					<div class="flex min-w-0 flex-1 flex-col justify-center">
						{#if selectedHeld}
							<span class="mb-1.5 font-mono text-[10px] uppercase tracking-[0.14em] text-accent-text">
								{i18n.status_in_library()}
							</span>
						{/if}
						<h2 class="text-[20px] font-bold leading-tight tracking-tight text-fg">{selected.title}</h2>
						{#if selected.book}
							<p class="mt-1 truncate text-[13px] text-fg-muted">{i18n.lookup_by({ name: selected.book.author })}</p>
						{/if}
						{#if selected.aside}
							<p class="mt-1 truncate text-[12.5px] italic text-fg-faint">{selected.aside}</p>
						{/if}
						<div class="mt-2.5 flex flex-wrap gap-1.5">
							{#each hitChips(selected) as c (c)}
								<span class="inline-flex h-6 items-center rounded-full border border-border bg-surface px-2.5 font-mono text-[11px] text-fg-muted">
									{c}
								</span>
							{/each}
						</div>
					</div>
				</div>
				{#if synopsis}
					<section class="mt-4">
						<h3 class="mb-2 font-mono text-[10.5px] uppercase tracking-[0.14em] text-fg-faint">
							{isArtist ? i18n.lookup_about() : i18n.detail_synopsis()}
						</h3>
						<p class="text-[13.5px] leading-relaxed text-fg-muted [text-wrap:pretty] {atFullHeight ? '' : 'line-clamp-3'}">
							{synopsis}
						</p>
					</section>
				{/if}
			{/if}
		{/snippet}

		{#snippet full()}
			<div class="pb-4">
				<MusicBookLookupPanel
					{kind}
					hit={selected}
					detail={detailQuery.data}
					loading={detailQuery.isLoading}
					error={detailQuery.isError ? errorText(detailQuery.error, i18n.torrent_details_failed()) : undefined}
					compact
					headless
				/>
			</div>
		{/snippet}

		{#snippet footer()}
			{#if selectedHeld}
				{#if selectedLocalId !== undefined && selected}
					<a
						href={libraryHref(selected, selectedLocalId)}
						onclick={onClose}
						class="flex h-[50px] items-center justify-center gap-2 rounded-xl border border-border bg-surface text-[16px] font-semibold text-fg-muted transition active:opacity-80"
					>
						{i18n.action_open_in_library()}
						<ArrowUpRight size={17} aria-hidden="true" />
					</a>
				{:else}
					<div class="flex h-[50px] items-center justify-center rounded-xl border border-border bg-surface text-[15px] text-fg-muted">
						{i18n.status_in_library()}
					</div>
				{/if}
			{:else if selectedRequested}
				<div
					class="flex h-[50px] items-center justify-center gap-2 rounded-xl border border-accent-line bg-accent-soft text-[16px] font-semibold text-accent-text"
				>
					<Check size={17} aria-hidden="true" />
					{i18n.status_requested()}
				</div>
			{:else}
				<div class="flex h-[46px] items-center justify-between gap-3 rounded-xl border border-border bg-surface pr-1.5 pl-3.5">
					<span class="flex-none text-[13.5px] text-fg">
						{canAdd ? i18n.quality_profile() : i18n.quality_preferred()}
					</span>
					<div class="w-[9.5rem]">
						<Select
							value={qualityProfileName}
							options={qpOptions}
							onChange={(v) => (qualityProfileName = v)}
							ariaLabel={canAdd ? i18n.quality_profile() : i18n.quality_preferred()}
						/>
					</div>
				</div>
				<button
					type="button"
					disabled={selectedPending}
					aria-busy={selectedPending}
					onclick={() => selected && addMutation.mutate(selected)}
					class="mt-2.5 flex h-[50px] w-full items-center justify-center gap-2 rounded-xl bg-accent text-[16px] font-semibold text-fg-on-accent transition active:opacity-80 disabled:opacity-60"
				>
					{#if selectedPending}
						<LoaderCircle size={17} class="animate-spin" aria-hidden="true" />
						{canAdd ? i18n.action_adding() : i18n.action_requesting()}
					{:else}
						<Plus size={18} aria-hidden="true" />
						{addLabel}
					{/if}
				</button>
			{/if}
		{/snippet}
	</LookupSheet>
{/if}
