<script lang="ts">
	import { createQuery, createMutation, useQueryClient } from "@tanstack/svelte-query";
	import {
		ArrowUpRight,
		BookOpen,
		Eye,
		Gauge,
		Layers,
		LoaderCircle,
		Music,
		Plus,
		Search,
		X,
	} from "@lucide/svelte";
	import { fade } from "svelte/transition";
	import { api, errorText } from "@lib/api";
	import { toast } from "@lib/toast";
	import { auth } from "@lib/auth.svelte";
	import type { QualityProfile } from "@lib/types";
	import { profilesPath } from "@lib/music-books";
	import {
		addErrorText,
		addRequest,
		libraryHref,
		libraryRoot,
		lookupDetail,
		lookupSearch,
		monitorOptions,
		profileMedia,
		requestBody,
		volumesCount,
		LOOKUP_SOURCE,
		hardcoverIssue,
		type HardcoverIssue,
		type LookupHit,
		type LookupKind,
		type LookupRelease,
	} from "@lib/music-books-lookup";
	import Modal from "@components/modals/Modal.svelte";
	import Select from "@components/forms/Select.svelte";
	import MusicBookLookupPanel from "./MusicBookLookupPanel.svelte";
	import ProviderKeyNotice from "./ProviderKeyNotice.svelte";
	import Img from "./Img.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The split add/request modal for an artist or a book, from md up: the same
	// layout as AddMovieModal and AddSeriesModal, searching MusicBrainz or
	// Hardcover. Below md AppShell mounts MusicBookLookupScreen instead.
	type Props = { kind: LookupKind; open: boolean; onClose: () => void };
	let { kind, open, onClose }: Props = $props();

	let isArtist = $derived(kind === "artist");
	let canAdd = $derived(auth.canAddDirectly);
	let media = $derived(profileMedia(kind));

	let query = $state("");
	let debounced = $state("");
	// "" means "let the backend resolve the default profile".
	let qualityProfileName = $state("");
	let monitor = $state("");
	let selectedKey = $state<string | null>(null);
	let showPanelOnNarrow = $state(false);
	let pendingKey = $state<string | null>(null);
	// Lookup key → library id for adds made during this session; covers the gap
	// until the next lookup marks the hit as already added.
	let sessionAdds = $state(new Map<string, number>());
	let requested = $state(new Set<string>());
	// The album picked off the highlighted artist's discography, for a request
	// of that album alone. It belongs to that artist, so it clears with it.
	let album = $state<LookupRelease | null>(null);
	let pendingAlbum: LookupRelease | null = null;
	const requestKey = (h: LookupHit, a: LookupRelease | null) => (a?.mbid ? `${h.key}:${a.mbid}` : h.key);
	$effect(() => {
		void selectedKey;
		album = null;
	});
	let failedImages = $state(new Set<string>());
	let searchInput = $state<HTMLInputElement | null>(null);
	let resultsList = $state<HTMLUListElement | null>(null);
	let debounceTimer: ReturnType<typeof setTimeout> | undefined;

	$effect(() => {
		const q = query;
		clearTimeout(debounceTimer);
		debounceTimer = setTimeout(() => {
			const next = q.trim();
			if (next !== debounced) refused = null;
			debounced = next;
		}, 300);
		return () => clearTimeout(debounceTimer);
	});

	$effect(() => {
		if (open) return;
		query = "";
		debounced = "";
		qualityProfileName = "";
		monitor = "";
		selectedKey = null;
		showPanelOnNarrow = false;
		refused = null;
		sessionAdds = new Map();
		requested = new Set();
		failedImages = new Set();
	});

	const qpQuery = createQuery<QualityProfile[]>(() => ({
		queryKey: ["quality-profiles", media],
		queryFn: () => api<QualityProfile[]>(profilesPath(media)),
		enabled: open,
	}));

	const searchQuery = createQuery<LookupHit[]>(() => ({
		queryKey: ["mb-lookup", kind, debounced],
		queryFn: () => lookupSearch(kind, debounced),
		enabled: open && debounced.length >= 2,
		staleTime: 60_000,
	}));

	// Hardcover refusing any call here (the search, a hit's detail, the add)
	// leaves the modal as a refused search does: the notice in place of the
	// results, nothing selected, no Add. Every call after it would fail the same
	// way, so the state holds until the next search: set where a call fails,
	// cleared where the next search starts, never from an effect.
	let refused = $state<HardcoverIssue | null>(null);
	let keyIssue = $derived(hardcoverIssue(searchQuery.error) ?? refused);
	let results = $derived(keyIssue ? [] : (searchQuery.data ?? []));
	let selected = $derived(results.find((r) => r.key === selectedKey));

	const detailQuery = createQuery(() => ({
		queryKey: ["mb-lookup-detail", selected?.key ?? null],
		queryFn: async () => {
			try {
				return await lookupDetail(selected!);
			} catch (e) {
				refused = hardcoverIssue(e) ?? refused;
				throw e;
			}
		},
		enabled: open && !!selected,
		staleTime: 5 * 60_000,
	}));

	// Keep the highlight on a row that still exists as results change.
	$effect(() => {
		const list = results;
		if (list.length === 0) {
			if (selectedKey !== null) selectedKey = null;
			return;
		}
		if (!list.some((r) => r.key === selectedKey)) selectedKey = list[0]?.key ?? null;
	});

	const qc = useQueryClient();
	const addMutation = createMutation<{ id: number } | null, Error, LookupHit>(() => ({
		onMutate: (h) => {
			pendingKey = h.key;
			pendingAlbum = canAdd ? null : album;
		},
		mutationFn: async (h) => {
			if (!canAdd) {
				await api("/requests", { method: "POST", body: requestBody(h, qualityProfileName, pendingAlbum) });
				return null;
			}
			const { path, body } = addRequest(h, qualityProfileName, effectiveMonitor);
			return api<{ id: number }>(path, { method: "POST", body });
		},
		onSuccess: (item, h) => {
			if (!canAdd || !item) {
				requested = new Set(requested).add(requestKey(h, pendingAlbum));
				qc.invalidateQueries({ queryKey: ["requests"] });
				toast.ok(i18n.toast_requested({ title: pendingAlbum?.title ?? h.title }));
				return;
			}
			sessionAdds = new Map(sessionAdds).set(h.key, item.id);
			qc.invalidateQueries({ queryKey: [libraryRoot(kind)] });
			toast.ok(i18n.toast_added({ title: h.title }));
		},
		onError: (e) => {
			const issue = hardcoverIssue(e);
			if (issue) refused = issue;
			else toast.err(addErrorText(e, canAdd));
		},
		onSettled: () => {
			pendingKey = null;
		},
	}));

	let qpOptions = $derived([
		{ value: "", label: canAdd ? i18n.quality_server_default() : i18n.quality_no_preference() },
		...(qpQuery.data ?? []).map((p) => ({ value: p.name, label: p.name })),
	]);
	// A book and a series monitor different things, so the choice resets to the
	// first option whenever the highlight moves to the other shape.
	let monitorOpts = $derived(monitorOptions(selected));
	let effectiveMonitor = $derived(
		monitorOpts.some((o) => o.value === monitor) ? monitor : (monitorOpts[0]?.value ?? ""),
	);

	const localId = (h: LookupHit) => h.library_id ?? sessionAdds.get(h.key);
	const isHeld = (h: LookupHit) => h.held || sessionAdds.has(h.key);
	let selectedLocalId = $derived(selected ? localId(selected) : undefined);
	let selectedHeld = $derived(selected ? isHeld(selected) : false);
	let selectedPending = $derived(selected ? pendingKey === selected.key : false);
	let selectedRequested = $derived(selected ? requested.has(requestKey(selected, album)) : false);

	let addLabel = $derived(
		!canAdd
			? album
				? i18n.action_request_album()
				: i18n.action_request()
			: isArtist
				? i18n.action_add_artist()
				: selected?.series
					? i18n.action_add_series()
					: i18n.action_add_book(),
	);

	function selectResult(h: LookupHit, revealPanel = false) {
		selectedKey = h.key;
		if (revealPanel) showPanelOnNarrow = true;
	}
	function rowButtons(): HTMLElement[] {
		return resultsList ? Array.from(resultsList.querySelectorAll<HTMLElement>("button[data-row]")) : [];
	}
	function onSearchKeydown(e: KeyboardEvent) {
		if (e.key === "ArrowDown" && results.length > 0) {
			e.preventDefault();
			rowButtons()[0]?.focus();
		}
	}
	function onResultsKeydown(e: KeyboardEvent) {
		if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
		const rows = rowButtons();
		const idx = rows.indexOf(document.activeElement as HTMLElement);
		if (idx < 0) return;
		e.preventDefault();
		if (e.key === "ArrowUp" && idx === 0) {
			searchInput?.focus();
			return;
		}
		const next = e.key === "ArrowDown" ? Math.min(idx + 1, rows.length - 1) : idx - 1;
		if (results[next]) selectResult(results[next]);
		rows[next]?.focus();
	}
	function onResultsFocusIn(e: FocusEvent) {
		const btn = (e.target as HTMLElement | null)?.closest?.("button[data-row]");
		if (!btn) return;
		const idx = rowButtons().indexOf(btn as HTMLElement);
		if (idx >= 0 && results[idx]) selectResult(results[idx]);
	}
	function markImageFailed(key: string) {
		failedImages = new Set(failedImages).add(key);
	}

	let title = $derived(
		canAdd
			? isArtist
				? i18n.action_add_artist()
				: i18n.action_add_book()
			: isArtist
				? i18n.action_request_artist_title()
				: i18n.action_request_book_title(),
	);
	let panelHit = $derived(
		selected && failedImages.has(selected.key) ? { ...selected, image: undefined } : selected,
	);
</script>

<Modal {open} {onClose} {title} size="3xl" footer={results.length > 0 ? actionFooter : undefined}>
	<div class="-mx-5 -my-4 grid min-h-[26rem] md:h-[60vh] md:max-h-[34rem] md:grid-cols-[340px_1fr]">
		<div class="min-h-0 flex-col gap-3 border-border p-4 md:flex md:border-r {showPanelOnNarrow ? 'hidden' : 'flex'}">
			<div class="relative">
				<Search class="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-fg-faint" aria-hidden="true" />
				<input
					type="search"
					bind:this={searchInput}
					bind:value={query}
					onkeydown={onSearchKeydown}
					placeholder={isArtist ? i18n.lookup_search_mb_placeholder() : i18n.lookup_search_hc_placeholder()}
					autocomplete="off"
					aria-label={isArtist ? i18n.lookup_search_mb() : i18n.lookup_search_hc()}
					class="w-full rounded-md border border-border bg-bg-card py-2 pl-10 pr-10 text-sm text-fg outline-none focus:border-accent focus:ring-2 focus:ring-accent-ring placeholder:text-fg-faint"
				/>
				{#if query.length > 0}
					<button
						type="button"
						onclick={() => {
							query = "";
							searchInput?.focus();
						}}
						aria-label={i18n.common_clear_search()}
						class="absolute right-2 top-1/2 grid h-7 w-7 -translate-y-1/2 place-items-center rounded text-fg-faint transition hover:bg-surface hover:text-fg"
					>
						<X size={14} aria-hidden="true" />
					</button>
				{/if}
			</div>

			{#if debounced.length >= 2 && !searchQuery.isLoading && !searchQuery.isError && results.length > 0}
				<p class="px-1 text-[11px] text-fg-faint">
					{(results.length === 1 ? i18n.lookup_matches_for_one : i18n.lookup_matches_for_other)({
						count: results.length,
						query: debounced,
					})}
				</p>
			{/if}

			<div class="flex min-h-0 flex-1 flex-col md:overflow-y-auto md:overscroll-contain">
				{#if debounced.length < 2}
					<div class="flex flex-1 flex-col items-center justify-center py-12 text-center">
						<Search class="mb-3 h-8 w-8 text-fg-faint" aria-hidden="true" />
						<p class="text-sm font-medium text-fg-muted">
							{isArtist ? i18n.lookup_mb_prompt() : i18n.lookup_hc_prompt()}
						</p>
						<p class="mt-1 text-xs text-fg-faint">
							{isArtist ? i18n.lookup_type_2_artist() : i18n.lookup_type_2_book()}
						</p>
					</div>
				{:else if searchQuery.isLoading}
					<ul class="space-y-2">
						{#each [0, 1, 2, 3] as i (i)}
							<li class="flex items-center gap-3 rounded-lg border border-border bg-bg-card p-2.5">
								<div class="w-11 flex-none animate-pulse bg-bg-deep {isArtist ? 'aspect-square rounded-full' : 'aspect-[2/3] rounded-md'}"></div>
								<div class="flex min-w-0 flex-1 flex-col justify-center gap-2">
									<div class="h-3 w-2/3 animate-pulse rounded bg-bg-deep"></div>
									<div class="h-2 w-1/2 animate-pulse rounded bg-bg-deep"></div>
								</div>
							</li>
						{/each}
					</ul>
				{:else if keyIssue}
					<ProviderKeyNotice reason={keyIssue} onNavigate={onClose} />
				{:else if searchQuery.isError}
					<p
						role="alert"
						class="rounded-lg border border-dashed border-status-failed/40 bg-status-failed/5 py-8 text-center text-xs text-status-failed"
					>
						{errorText(searchQuery.error, i18n.common_search_failed())}
					</p>
				{:else if results.length === 0}
					<div class="flex flex-1 flex-col items-center justify-center py-12 text-center">
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
					<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
					<ul bind:this={resultsList} onkeydown={onResultsKeydown} onfocusin={onResultsFocusIn} class="space-y-2">
						{#each results as r (r.key)}
							{@const held = isHeld(r)}
							{@const isSel = r.key === selectedKey}
							<li in:fade={{ duration: 140 }}>
								<button
									type="button"
									data-row
									aria-pressed={isSel}
									onclick={() => selectResult(r, true)}
									class="flex w-full items-center gap-3 rounded-lg border p-2.5 text-left transition focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring {isSel
										? 'border-accent-line bg-accent-soft'
										: held
											? 'border-accent/40 bg-accent/5 hover:border-border-strong'
											: 'border-border bg-bg-card hover:border-border-strong hover:bg-bg-elevated'}"
								>
									<div
										class="relative w-11 flex-none overflow-hidden border border-white/[0.06] bg-bg-deep shadow-1 {isArtist
											? 'aspect-square rounded-full'
											: 'aspect-[2/3] rounded-md'}"
									>
										<div class="absolute inset-0 grid place-items-center text-fg-faint">
											{#if isArtist}
												<Music class="h-4 w-4" aria-hidden="true" />
											{:else}
												<BookOpen class="h-4 w-4" aria-hidden="true" />
											{/if}
										</div>
										{#if r.image && !failedImages.has(r.key)}
											<Img src={r.image} alt="" onerror={() => markImageFailed(r.key)} class="relative h-full w-full object-cover" />
										{/if}
									</div>
									<div class="flex min-w-0 flex-1 flex-col justify-center">
										<span class="block truncate text-[13.5px] font-semibold text-fg">
											{r.title}
											{#if r.year && !isArtist}
												<span class="ml-1 font-mono text-[11px] font-normal text-fg-subtle">· {r.year}</span>
											{/if}
										</span>
										{#if held}
											<span
												class="mt-1 inline-flex w-fit items-center rounded-full border border-border bg-bg-elevated px-2 py-0.5 font-mono text-[10px] uppercase tracking-[0.1em] text-fg-subtle"
											>
												{i18n.status_in_library()}
											</span>
										{:else}
											<span class="mt-0.5 flex min-w-0 items-center gap-1.5 text-[11.5px] text-fg-muted">
												<span class="truncate">{r.aside && isArtist ? r.aside : r.subtitle}</span>
												{#if r.series && r.book?.volumes}
													<span class="inline-flex shrink-0 items-center gap-1 font-mono text-[10.5px] text-fg-subtle">
														<Layers size={11} aria-hidden="true" />
														{volumesCount(r.book.volumes)}
													</span>
												{/if}
											</span>
										{/if}
									</div>
								</button>
							</li>
						{/each}
					</ul>
				{/if}
			</div>
		</div>

		<div class="min-h-0 md:overflow-y-auto md:overscroll-contain {showPanelOnNarrow ? 'block' : 'hidden md:block'}">
			<MusicBookLookupPanel
				{kind}
				hit={panelHit}
				detail={detailQuery.data}
				loading={detailQuery.isLoading}
				error={detailQuery.isError ? errorText(detailQuery.error, i18n.torrent_details_failed()) : undefined}
				onBack={() => (showPanelOnNarrow = false)}
				albumPick={!canAdd && isArtist
					? { selected: album?.mbid ?? null, onToggle: (r) => (album = album?.mbid === r.mbid ? null : r) }
					: undefined}
			/>
		</div>
	</div>
</Modal>

{#snippet actionFooter()}
	<div class="mr-auto flex flex-wrap items-center gap-x-5 gap-y-2">
		<div class="flex items-center gap-2">
			<label for="add-{kind}-qp" class="inline-flex shrink-0 items-center gap-1.5 text-sm font-medium text-fg">
				<Gauge size={16} class="text-fg-muted" aria-hidden="true" />
				{canAdd ? i18n.common_quality() : i18n.quality_preferred()}
			</label>
			<div class="w-44">
				<Select id="add-{kind}-qp" value={qualityProfileName} options={qpOptions} onChange={(v) => (qualityProfileName = v)} />
			</div>
		</div>
		{#if canAdd}
			<div class="flex items-center gap-2">
				<label for="add-{kind}-monitor" class="inline-flex items-center gap-1.5 text-sm font-medium text-fg">
					<Eye size={16} class="text-fg-muted" aria-hidden="true" />
					{i18n.action_monitor()}
				</label>
				<div class="w-52">
					<Select id="add-{kind}-monitor" value={effectiveMonitor} options={monitorOpts} onChange={(v) => (monitor = v)} />
				</div>
			</div>
		{/if}
	</div>

	{#if selected}
		{#if selectedHeld && !album && selectedLocalId !== undefined}
			<a
				href={libraryHref(selected, selectedLocalId)}
				onclick={onClose}
				class="inline-flex min-h-11 lg:h-9 lg:min-h-0 items-center gap-1.5 rounded-md border border-border bg-bg-elevated px-4 text-sm font-medium text-fg-muted transition hover:border-border-strong hover:text-fg"
			>
				{i18n.action_open_in_library()}
				<ArrowUpRight size={15} aria-hidden="true" />
			</a>
		{:else if selectedHeld && !album}
			<span class="inline-flex h-9 items-center rounded-md border border-border bg-bg-elevated px-4 text-sm font-medium text-fg-muted">
				{i18n.status_in_library()}
			</span>
		{:else if selectedRequested}
			<span class="inline-flex h-9 items-center rounded-md border border-accent-line bg-accent-soft px-4 text-sm font-medium text-accent-text">
				{i18n.status_requested()}
			</span>
		{:else}
			<button
				type="button"
				disabled={selectedPending}
				aria-busy={selectedPending}
				onclick={() => selected && addMutation.mutate(selected)}
				class="inline-flex min-h-11 lg:h-9 lg:min-h-0 items-center gap-1.5 rounded-md bg-accent px-4 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
			>
				{#if selectedPending}
					<LoaderCircle size={15} class="animate-spin" aria-hidden="true" />
					{canAdd ? i18n.action_adding() : i18n.action_requesting()}
				{:else}
					<Plus size={15} aria-hidden="true" />
					{addLabel}
				{/if}
			</button>
		{/if}
	{/if}
{/snippet}
