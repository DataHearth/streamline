<script lang="ts">
	import { createQuery, createMutation } from "@tanstack/svelte-query";
	import {
		TriangleAlert,
		ChevronDown,
		ChevronUp,
		Download,
		History,
		Info,
		LoaderCircle,
	} from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import { api, errorText } from "@lib/api";
	import { auth } from "@lib/auth.svelte";
	import { toast } from "@lib/toast";
	import { formatBytes } from "@lib/format";
	import { loadPref, savePref } from "@lib/prefs";
	import {
		langOf,
		langLabel,
		langHelp,
		LANG_CHIP,
		qualityWord,
		speedOf,
		speedLabel,
		SPEED_CLASS,
		SPEED_DOT,
		recommendedOf,
		type ExistingFile,
		type LangKind,
	} from "@lib/release-facts";
	import type { Indexer, SearchResult } from "@lib/types";
	import Select from "@components/forms/Select.svelte";
	import Dialog from "@components/modals/Dialog.svelte";
	import SearchNarration from "@components/shared/SearchNarration.svelte";
	import { getLocale } from "@lib/paraglide/runtime.js";
	import { m as i18n } from "@lib/paraglide/messages.js";

	type Field =
		| "title"
		| "size"
		| "seeders"
		| "source"
		| "codec"
		| "group"
		| "indexer"
		| "published"
		| "score";
	type Dir = "asc" | "desc";

	let {
		searchPath,
		grabPath,
		queryKey,
		enabled = true,
		existing = null,
		existingCount,
		onGrabbed,
	}: {
		// POST endpoint returning ranked SearchResult[] (movie or episode browse).
		searchPath: string;
		// POST endpoint that grabs the chosen release (body is the SearchResult).
		grabPath: string;
		queryKey: readonly unknown[];
		enabled?: boolean;
		// The file a grab would replace, when there is exactly one (a movie, an
		// episode). Shown beside the pick in the confirmation.
		existing?: ExistingFile | null;
		// How many files the scope already has on disk. Defaults to one when
		// `existing` is set; a season or whole-show search passes a count and no
		// file. Anything above zero asks before grabbing, and a confirmed grab
		// carries replace_existing.
		existingCount?: number;
		onGrabbed?: () => void;
	} = $props();

	// The friendly view is everyone's default; the table is one switch away
	// and the choice sticks per browser.
	const TECH_PREF = "streamline:releases:technical";
	let technical = $state(loadPref(TECH_PREF) === "1");
	function setTechnical(v: boolean) {
		technical = v;
		savePref(TECH_PREF, v ? "1" : "0");
	}

	let sortField = $state<Field>("seeders");
	let sortDir = $state<Dir>("desc");
	// The API already ranks scored results best-first, so a scored search lands
	// on score-descending — but only until the operator picks a column, or the
	// arriving page would keep yanking their sort back.
	let sortPicked = $state(false);
	let groupFilter = $state<string>("");
	let indexerFilter = $state<string>("");
	let errMsg = $state<string | null>(null);
	// Cancelling is local: the endpoint answers once, so there is nothing to
	// abort server-side. Dropping the query is what "stop waiting" means here,
	// and it leaves a state that says so rather than an empty table.
	let canceled = $state(false);
	$effect(() => {
		// Reopening the dialog is a fresh search, never a cancelled one.
		if (enabled) canceled = false;
	});

	type Results = { items: SearchResult[]; hiddenPacks: number };

	const q = createQuery<Results>(() => ({
		queryKey,
		// Movie search returns a bare array; series episode search returns an
		// { items, hidden_packs } envelope. Normalize both.
		queryFn: async () => {
			const raw = await api<
				SearchResult[] | { items?: SearchResult[]; hidden_packs?: number }
			>(searchPath, { method: "POST" });
			if (Array.isArray(raw)) return { items: raw, hiddenPacks: 0 };
			return { items: raw.items ?? [], hiddenPacks: raw.hidden_packs ?? 0 };
		},
		enabled: enabled && !canceled,
		staleTime: 30_000,
	}));

	// Only to name the scope of the wait: "Searching 6 indexers". Admin-only
	// endpoint, so a non-admin gets the countless phrasing rather than a number
	// they could not check. Silent: the global bar belongs to the search.
	const indexersQuery = createQuery<Indexer[]>(() => ({
		queryKey: ["indexers"],
		queryFn: () => api<Indexer[]>("/indexers"),
		enabled:
			enabled && !canceled && q.isLoading && auth.user?.role === "admin",
		staleTime: 300_000,
		meta: { silent: true },
	}));
	let indexerCount = $derived(
		indexersQuery.data?.filter((i) => i.enabled).length || undefined,
	);

	let data = $derived(q.data?.items ?? []);
	// Packs the requested scope excluded. Absent on every scope but the episode
	// one, which is why an empty list there is worth explaining.
	let hiddenPacks = $derived(q.data?.hiddenPacks ?? 0);

	function fieldValue(r: SearchResult, f: Field): string | number {
		switch (f) {
			case "title":
				return r.title;
			case "size":
				return r.size;
			case "seeders":
				return r.seeders;
			case "source":
				return r.source ?? "";
			case "codec":
				return r.codec ?? "";
			case "group":
				return r.release_group ?? "";
			case "indexer":
				return r.indexer ?? "";
			case "published":
				return r.published_at ? Date.parse(r.published_at) : 0;
			case "score":
				return r.score ?? 0;
		}
	}

	// Scores are absent wholesale when no quality profile resolves for the item,
	// so one row carrying one is what turns the column on.
	let scored = $derived(data.some((r) => r.score !== undefined));

	$effect(() => {
		if (scored && !sortPicked) {
			sortField = "score";
			sortDir = "desc";
		}
	});

	function scoreClass(n: number): string {
		if (n > 0) return "text-status-available";
		if (n < 0) return "text-status-failed";
		return "text-fg-muted";
	}

	// Distinct values present in the current results, for the filter dropdowns.
	function distinct(pick: (r: SearchResult) => string | undefined): string[] {
		const set = new Set<string>();
		for (const r of data) {
			const v = pick(r);
			if (v) set.add(v);
		}
		return [...set].sort((a, b) => a.localeCompare(b));
	}
	let groups = $derived(distinct((r) => r.release_group));
	let indexers = $derived(distinct((r) => r.indexer));

	let groupOptions = $derived([
		{ value: "", label: i18n.movies_all_groups() },
		...groups.map((g) => ({ value: g, label: g })),
	]);
	let indexerOptions = $derived([
		{ value: "", label: i18n.movies_all_indexers() },
		...indexers.map((ix) => ({ value: ix, label: ix })),
	]);

	let rows = $derived.by(() => {
		const arr = data.filter(
			(r) =>
				(groupFilter === "" || r.release_group === groupFilter) &&
				(indexerFilter === "" || r.indexer === indexerFilter),
		);
		const mul = sortDir === "asc" ? 1 : -1;
		arr.sort((a, b) => {
			const av = fieldValue(a, sortField);
			const bv = fieldValue(b, sortField);
			if (typeof av === "number" && typeof bv === "number")
				return mul * (av - bv);
			return mul * String(av).localeCompare(String(bv));
		});
		return arr;
	});

	// ── friendly view ─────────────────────────────────────────────────────────
	// No sort controls and no filters: best first (score, or seeders when the
	// item has no profile), the recommended one on top, the profile's
	// rejections folded away.
	let recommended = $derived(recommendedOf(data));
	let friendly = $derived.by(() => {
		const key = (r: SearchResult) => (scored ? (r.score ?? 0) : r.seeders);
		const kept = data
			.filter((r) => !r.rejected)
			.sort((a, b) => key(b) - key(a) || b.seeders - a.seeders);
		const rec = recommended;
		return rec ? [rec, ...kept.filter((r) => r !== rec)] : kept;
	});
	let setAside = $derived(data.filter((r) => r.rejected));
	let showSetAside = $state(false);
	// The language chip explains itself on hover; a tap pins the explanation
	// under the chips, which is the only way a touch screen gets it.
	let langOpen = $state<string | null>(null);

	function lang(r: SearchResult): LangKind {
		return langOf(r.title);
	}

	// ── grab, and the question before replacing ──────────────────────────────
	type GrabVars = { r: SearchResult; replace: boolean };
	const grab = createMutation<unknown, Error, GrabVars>(() => ({
		mutationFn: ({ r, replace }) =>
			api(grabPath, {
				method: "POST",
				body: replace ? { ...r, replace_existing: true } : r,
			}),
		onSuccess: (_d, { r }) => {
			toast.ok(i18n.toast_grabbed({ title: r.title }));
			onGrabbed?.();
		},
		onError: (e) => {
			errMsg = errorText(e, i18n.grab_failed());
			toast.err(errMsg);
		},
	}));

	let replaceCount = $derived(existingCount ?? (existing ? 1 : 0));
	let confirmFor = $state<SearchResult | null>(null);

	let pendingId = $state<string | null>(null);
	function doGrab(r: SearchResult, replace: boolean) {
		pendingId = r.download_url;
		grab.mutate(
			{ r, replace },
			{
				onSettled: () => {
					pendingId = null;
				},
			},
		);
	}
	function onSelect(r: SearchResult) {
		if (replaceCount > 0) confirmFor = r;
		else doGrab(r, false);
	}

	// Relative age, e.g. "3h", "5d", "2mo" — the at-a-glance recency signal that
	// matters when picking a release. Absolute timestamp lives in the cell title.
	function fmtAge(iso?: string): string {
		if (!iso) return "—";
		const t = Date.parse(iso);
		if (Number.isNaN(t)) return "—";
		const s = Math.max(0, (Date.now() - t) / 1000);
		if (s < 60) return i18n.age_now();
		const m = s / 60;
		if (m < 60) return i18n.age_minutes({ n: Math.floor(m) });
		const h = m / 60;
		if (h < 24) return i18n.age_hours({ n: Math.floor(h) });
		const d = h / 24;
		if (d < 30) return i18n.age_days({ n: Math.floor(d) });
		const mo = d / 30;
		if (mo < 12) return i18n.age_months({ n: Math.floor(mo) });
		return i18n.age_years({ n: Math.floor(d / 365) });
	}

	function fmtDate(iso?: string): string {
		if (!iso) return i18n.unknown_release_date();
		const t = Date.parse(iso);
		if (Number.isNaN(t)) return i18n.unknown_release_date();
		return new Date(t).toLocaleString();
	}

	function fmtDay(iso: string): string {
		const t = Date.parse(iso);
		if (Number.isNaN(t)) return "";
		return new Date(t).toLocaleDateString(getLocale(), {
			day: "numeric",
			month: "short",
		});
	}

	function seederClass(n: number): string {
		if (n >= 50) return "text-status-available";
		if (n >= 10) return "text-status-wanted";
		return "text-status-failed";
	}

	function ariaSort(f: Field): "ascending" | "descending" | "none" {
		if (sortField !== f) return "none";
		return sortDir === "asc" ? "ascending" : "descending";
	}

	function toggle(f: Field) {
		sortPicked = true;
		if (sortField === f) sortDir = sortDir === "asc" ? "desc" : "asc";
		else {
			sortField = f;
			sortDir = f === "title" ? "asc" : "desc";
		}
	}
</script>

{#snippet packsHidden()}
	{#if hiddenPacks > 0}
		<p class="mb-3 flex items-start gap-1.5 text-xs text-fg-subtle">
			<Info size={13} class="mt-px shrink-0" aria-hidden="true" />
			<span>{i18n.grab_packs_hidden({ count: hiddenPacks })}</span>
		</p>
	{/if}
{/snippet}

{#snippet sortIcon(f: Field)}
	{#if sortField === f}
		{#if sortDir === "asc"}
			<ChevronUp size={12} aria-hidden="true" />
		{:else}
			<ChevronDown size={12} aria-hidden="true" />
		{/if}
	{/if}
{/snippet}

{#snippet langStatic(k: LangKind)}
	<span
		class={cn(
			"inline-flex h-6 whitespace-nowrap items-center rounded-sm px-2 text-[12px] font-semibold",
			LANG_CHIP[k],
		)}
	>
		{langLabel(k)}
	</span>
{/snippet}

{#snippet langChip(r: SearchResult)}
	{@const k = lang(r)}
	<span class="group/lang relative inline-flex">
		<button
			type="button"
			onclick={() => (langOpen = langOpen === r.download_url ? null : r.download_url)}
			aria-expanded={langOpen === r.download_url}
			aria-label={`${langLabel(k)} — ${langHelp(k)}`}
			class={cn(
				"inline-flex h-6 whitespace-nowrap cursor-help items-center rounded-sm px-2 text-[12px] font-semibold",
				LANG_CHIP[k],
			)}
		>
			{langLabel(k)}
		</button>
		<span
			role="tooltip"
			class="pointer-events-none invisible absolute left-0 top-full z-20 mt-1.5 w-60 rounded-md border border-border-strong bg-bg-card p-2.5 text-[12px] leading-snug text-fg-muted opacity-0 shadow-3 transition-opacity group-hover/lang:visible group-hover/lang:opacity-100"
		>
			<strong class="block font-semibold text-fg">{langLabel(k)}</strong>
			{langHelp(k)}
		</span>
	</span>
{/snippet}

{#snippet seenChip(iso: string)}
	<span
		class="inline-flex h-6 whitespace-nowrap items-center gap-1 rounded-sm border border-status-completed/35 px-2 text-[12px] text-status-completed"
		title={i18n.releases_seen_help()}
	>
		<History size={12} aria-hidden="true" />
		{i18n.releases_seen({ date: fmtDay(iso) })}
	</span>
{/snippet}

{#snippet selectButton(r: SearchResult, extra = "")}
	{@const pending = pendingId === r.download_url}
	<button
		type="button"
		onclick={() => onSelect(r)}
		disabled={pending || grab.isPending}
		class={cn(
			"inline-flex min-h-11 shrink-0 items-center gap-1.5 rounded-md bg-accent px-3 text-[12.5px] font-semibold text-fg-on-accent transition hover:bg-accent-hover active:bg-accent-pressed disabled:cursor-not-allowed disabled:opacity-60 lg:h-9 lg:min-h-0",
			extra,
		)}
	>
		{#if pending}
			<LoaderCircle size={13} class="animate-spin" aria-hidden="true" />
		{:else}
			<Download size={13} aria-hidden="true" />
		{/if}
		{i18n.releases_select()}
	</button>
{/snippet}

{#snippet friendlyCard(r: SearchResult, isRec: boolean)}
	{@const k = lang(r)}
	{@const quality = qualityWord(r)}
	{@const speed = speedOf(r.seeders)}
	<!-- One button, two placements: beside the facts from md, at the end of the
	     meta line below it. Grid areas move it rather than a second copy. -->
	<li
		class={cn(
			"grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-2.5 rounded-lg border p-3 [grid-template-areas:'info_info''meta_act'] md:px-3.5 md:[grid-template-areas:'info_act''meta_act']",
			isRec
				? "border-accent-line bg-accent-soft/40"
				: "border-border bg-bg-base",
			r.rejected && "opacity-60",
		)}
	>
		<div class="min-w-0 [grid-area:info]">
			{#if isRec}
				<div class="mb-2 flex flex-wrap items-center gap-x-2 gap-y-1">
					<span
						class="inline-flex h-5 whitespace-nowrap items-center rounded-full bg-accent px-2 text-[11px] font-semibold text-fg-on-accent"
					>
						★ {i18n.releases_recommended()}
					</span>
					<span class="text-[11.5px] text-accent-text">
						{i18n.releases_recommended_why()}
					</span>
				</div>
			{/if}
			<div class="flex flex-wrap items-center gap-1.5">
				{@render langChip(r)}
				{#if quality}
					<span
						class="inline-flex h-6 whitespace-nowrap items-center rounded-sm bg-bg-card px-2 text-[12px] font-medium text-fg-muted"
					>
						{quality}
					</span>
				{/if}
				{#if r.indexer_private === true}
					<span
						class="inline-flex h-6 whitespace-nowrap items-center rounded-sm border border-status-seeding/35 bg-status-seeding/10 px-2 text-[12px] font-medium text-status-seeding"
					>
						{i18n.releases_private()}
					</span>
				{:else if r.indexer_private === false}
					<span
						class="inline-flex h-6 whitespace-nowrap items-center rounded-sm border border-border-strong px-2 text-[12px] font-medium text-fg-subtle"
					>
						{i18n.releases_public()}
					</span>
				{/if}
				{#if r.previously_grabbed_at}
					{@render seenChip(r.previously_grabbed_at)}
				{/if}
			</div>
			{#if langOpen === r.download_url}
				<p
					class="mt-2 rounded-md bg-bg-card px-2.5 py-2 text-[12px] leading-snug text-fg-muted"
				>
					<strong class="font-semibold text-fg">{langLabel(k)}</strong>
					· {langHelp(k)}
				</p>
			{/if}
			{#if r.rejected}
				<p class="mt-1.5 text-[11.5px] text-status-failed">
					{r.reject_reason ?? i18n.release_rejected()}
				</p>
			{/if}
		</div>
		<div
			class="flex min-w-0 flex-wrap items-center gap-x-3.5 gap-y-1 text-[12px] tabular text-fg-subtle [grid-area:meta] [&>*]:whitespace-nowrap"
		>
			<span class="font-medium text-fg-muted">{formatBytes(r.size)}</span>
			<span
				class={cn("inline-flex items-center gap-1.5", SPEED_CLASS[speed])}
				title={i18n.releases_speed_help({ count: r.seeders })}
			>
				<span class={cn("h-[7px] w-[7px] rounded-full", SPEED_DOT[speed])} aria-hidden="true"
				></span>
				{speedLabel(speed)}
			</span>
			<span title={fmtDate(r.published_at)}>{fmtAge(r.published_at)}</span>
			{#if r.release_group}
				<span class="font-mono text-[11px] text-fg-faint">{r.release_group}</span>
			{/if}
		</div>
		{@render selectButton(r, "[grid-area:act] self-end md:self-center")}
	</li>
{/snippet}

{#if errMsg}
	<div
		role="alert"
		class="mb-3 flex items-start gap-2 rounded-md border border-status-failed/40 bg-status-failed/10 p-2 text-xs text-status-failed"
	>
		<TriangleAlert class="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden="true" />
		{errMsg}
	</div>
{/if}

{#if canceled}
	<div
		class="rounded-lg border border-dashed border-border bg-bg-elevated px-5 py-10 text-center"
	>
		<p class="text-sm text-fg-muted">{i18n.search_canceled()}</p>
		<button
			type="button"
			onclick={() => (canceled = false)}
			class="mt-2 text-xs font-medium text-accent transition hover:text-accent-hover"
		>
			{i18n.common_retry()}
		</button>
	</div>
{:else if q.isLoading}
	<SearchNarration
		{indexerCount}
		scopePending={indexersQuery.isPending && auth.user?.role === "admin"}
		onCancel={() => (canceled = true)}
	/>
{:else if q.isError}
	<div
		role="alert"
		class="rounded-lg border border-dashed border-status-failed/40 bg-status-failed/5 px-5 py-10 text-center"
	>
		<p class="text-sm text-status-failed">
			{errorText(q.error, i18n.common_search_failed())}
		</p>
		<button
			type="button"
			onclick={() => q.refetch()}
			class="mt-2 text-xs font-medium text-accent transition hover:text-accent-hover"
		>
			{i18n.common_retry()}
		</button>
	</div>
{:else if data.length === 0}
	<div
		class="rounded-lg border border-dashed border-border bg-bg-elevated px-5 py-10 text-center"
	>
		<p class="text-sm text-fg-muted">{i18n.grab_no_releases()}</p>
	</div>
	{@render packsHidden()}
{:else}
	{@render packsHidden()}
	<div class="mb-3 flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
		<span class="tabular text-[11px] text-fg-faint">
			{#if technical}
				{i18n.releases_count_of({ visible: rows.length, total: data.length })}
			{:else}
				{(friendly.length === 1 ? i18n.releases_results_one : i18n.releases_results_other)({
					count: friendly.length,
				})}{#if setAside.length > 0}{" · "}{(setAside.length === 1
						? i18n.releases_set_aside_one
						: i18n.releases_set_aside_other)({ count: setAside.length })}{/if}
			{/if}
		</span>
		<div class="flex flex-wrap items-center gap-2">
			{#if technical && groups.length > 0}
				<div class="w-40">
					<Select
						value={groupFilter}
						options={groupOptions}
						onChange={(v) => (groupFilter = v)}
						ariaLabel={i18n.releases_filter_group()}
					/>
				</div>
			{/if}
			{#if technical && indexers.length > 0}
				<div class="w-40">
					<Select
						value={indexerFilter}
						options={indexerOptions}
						onChange={(v) => (indexerFilter = v)}
						ariaLabel={i18n.releases_filter_indexer()}
					/>
				</div>
			{/if}
			<button
				type="button"
				role="switch"
				aria-checked={technical}
				onclick={() => setTechnical(!technical)}
				class="touch-hit inline-flex items-center gap-2 text-[12px] text-fg-muted transition hover:text-fg"
			>
				{i18n.releases_view_technical()}
				<span
					class={cn(
						"relative h-[18px] w-[30px] shrink-0 rounded-full border transition",
						technical
							? "border-accent-line bg-accent-soft"
							: "border-border-strong bg-surface-2",
					)}
					aria-hidden="true"
				>
					<span
						class={cn(
							"absolute top-[2px] h-3 w-3 rounded-full transition-all",
							technical ? "left-[14px] bg-accent" : "left-[2px] bg-fg-subtle",
						)}
					></span>
				</span>
			</button>
		</div>
	</div>

	{#if !technical}
		<ul class="flex max-h-[60vh] flex-col gap-2 overflow-y-auto overscroll-contain">
			{#each friendly as r (r.download_url)}
				{@render friendlyCard(r, r === recommended)}
			{/each}
			{#if setAside.length > 0}
				<li>
					<button
						type="button"
						onclick={() => (showSetAside = !showSetAside)}
						aria-expanded={showSetAside}
						class="flex min-h-11 w-full items-center justify-between gap-3 rounded-lg border border-dashed border-border-strong px-3.5 text-left text-[12px] text-fg-subtle transition hover:text-fg-muted lg:min-h-10"
					>
						<span>
							{(setAside.length === 1
								? i18n.releases_set_aside_long_one
								: i18n.releases_set_aside_long_other)({ count: setAside.length })}
						</span>
						<span class="shrink-0 text-accent-text">
							{showSetAside ? i18n.releases_fold_hide() : i18n.releases_fold_show()}
						</span>
					</button>
				</li>
				{#if showSetAside}
					{#each setAside as r (r.download_url)}
						{@render friendlyCard(r, false)}
					{/each}
				{/if}
			{/if}
		</ul>
	{:else if rows.length === 0}
		<div
			class="rounded-lg border border-dashed border-border bg-bg-elevated py-10 text-center text-sm text-fg-muted"
		>
			<p>{i18n.grab_no_releases_filtered()}</p>
			<button
				type="button"
				onclick={() => {
					groupFilter = "";
					indexerFilter = "";
				}}
				class="mt-2 text-xs font-medium text-accent transition hover:text-accent-hover"
			>
				{i18n.common_clear_filters()}
			</button>
		</div>
	{:else}
		<!-- Phone: cards. A row has seven columns of which only two fit, and
		     sideways scrolling to compare seeders is not a comparison. -->
		<ul
			class="flex max-h-[60vh] flex-col gap-2 overflow-y-auto overscroll-contain md:hidden"
		>
			{#each rows as r (r.download_url)}
				<li
					class={cn(
						"rounded-lg border border-border bg-bg-elevated p-3",
						r.rejected && "opacity-60",
					)}
				>
					<div class="break-all font-mono text-[12px] leading-snug text-fg [text-wrap:pretty]">
						{r.title}
					</div>
					{#if r.rejected}
						<p class="mt-1 text-[11px] text-status-failed">
							{i18n.release_rejected()}{r.reject_reason
								? ` · ${r.reject_reason}`
								: ""}
						</p>
					{/if}
					{#if r.previously_grabbed_at}
						<p class="mt-1 flex items-center gap-1 text-[11px] text-status-completed">
							<History size={11} aria-hidden="true" />
							{i18n.releases_seen({ date: fmtDay(r.previously_grabbed_at) })}
						</p>
					{/if}
					<div class="mt-1.5 flex flex-wrap gap-1">
						<span class="rounded-sm bg-bg-card px-1.5 py-px font-mono text-[10px] font-semibold text-fg">
							{langLabel(lang(r))}
						</span>
						{#if r.score !== undefined}
							<span
								class={cn(
									"rounded-sm bg-bg-card px-1.5 py-px font-mono text-[10px] tabular font-semibold",
									scoreClass(r.score),
								)}
								title={i18n.release_score_help()}
							>
								{i18n.release_score()}
								{r.score}
							</span>
						{/if}
						{#each r.matched_formats ?? [] as f (f)}
							<span
								class="rounded-sm bg-accent-soft px-1.5 py-px font-mono text-[10px] text-accent-text"
							>
								{f}
							</span>
						{/each}
						{#if r.resolution}
							<span class="rounded-sm bg-bg-card px-1.5 py-px font-mono text-[10px] text-fg-muted">
								{r.resolution}
							</span>
						{/if}
						{#if r.source}
							<span class="rounded-sm bg-bg-card px-1.5 py-px font-mono text-[10px] text-fg-muted">
								{r.source}
							</span>
						{/if}
						{#if r.codec}
							<span class="rounded-sm bg-bg-card px-1.5 py-px font-mono text-[10px] text-fg-muted">
								{r.codec}
							</span>
						{/if}
						{#if r.release_group}
							<span class="rounded-sm bg-bg-card px-1.5 py-px font-mono text-[10px] text-fg-muted">
								{r.release_group}
							</span>
						{/if}
					</div>
					<div class="mt-2.5 flex items-center gap-3">
						<span
							class={cn(
								"shrink-0 font-mono text-[11.5px] tabular font-medium",
								seederClass(r.seeders),
							)}
						>
							▲ {r.seeders}
						</span>
						<span class="shrink-0 font-mono text-[11.5px] tabular text-fg-muted">
							{formatBytes(r.size)}
						</span>
						<span
							class="min-w-0 truncate font-mono text-[11.5px] text-fg-subtle"
							title={fmtDate(r.published_at)}
						>
							{fmtAge(r.published_at)} · {r.indexer ?? "—"}
						</span>
						{@render selectButton(r, "ml-auto")}
					</div>
				</li>
			{/each}
		</ul>

		<div
			class="hidden max-h-[60vh] overflow-auto rounded-lg border border-border bg-bg-elevated md:block"
		>
			<table
				class={cn(
					"w-full table-fixed text-sm",
					scored ? "min-w-[840px]" : "min-w-[760px]",
				)}
			>
				<thead
					class="sticky top-0 z-10 bg-bg-elevated text-[10px] uppercase tracking-[0.12em] text-fg-faint [&_th]:bg-surface"
				>
					<tr class="border-b border-border">
						<th
							scope="col"
							aria-sort={ariaSort("title")}
							class="px-4 py-2.5 text-left font-medium"
						>
							<button
								type="button"
								onclick={() => toggle("title")}
								class="touch-hit inline-flex min-w-11 items-center justify-center gap-1 uppercase tracking-[0.12em] transition hover:text-fg"
							>
								{i18n.releases_col_result()}
								{@render sortIcon("title")}
							</button>
						</th>
						<th scope="col" class="w-20 px-3 py-2.5 text-left font-medium">
							{i18n.common_language()}
						</th>
						<th
							scope="col"
							aria-sort={ariaSort("group")}
							class="hidden w-24 px-3 py-2.5 text-left font-medium md:table-cell"
						>
							<button
								type="button"
								onclick={() => toggle("group")}
								class="touch-hit inline-flex min-w-11 items-center justify-center gap-1 uppercase tracking-[0.12em] transition hover:text-fg"
							>
								{i18n.file_group()}
								{@render sortIcon("group")}
							</button>
						</th>
						<th
							scope="col"
							aria-sort={ariaSort("indexer")}
							class="hidden w-28 px-3 py-2.5 text-left font-medium lg:table-cell"
						>
							<button
								type="button"
								onclick={() => toggle("indexer")}
								class="touch-hit inline-flex min-w-11 items-center justify-center gap-1 uppercase tracking-[0.12em] transition hover:text-fg"
							>
								{i18n.common_indexer()}
								{@render sortIcon("indexer")}
							</button>
						</th>
						<th
							scope="col"
							aria-sort={ariaSort("published")}
							class="hidden w-20 px-3 py-2.5 text-right font-medium sm:table-cell"
						>
							<button
								type="button"
								onclick={() => toggle("published")}
								class="touch-hit inline-flex min-w-11 items-center justify-center gap-1 uppercase tracking-[0.12em] transition hover:text-fg"
							>
								{i18n.releases_col_released()}
								{@render sortIcon("published")}
							</button>
						</th>
						{#if scored}
							<th
								scope="col"
								aria-sort={ariaSort("score")}
								class="w-20 px-3 py-2.5 text-right font-medium"
							>
								<button
									type="button"
									onclick={() => toggle("score")}
									title={i18n.release_score_help()}
									class="touch-hit inline-flex min-w-11 items-center justify-center gap-1 uppercase tracking-[0.12em] transition hover:text-fg"
								>
									{i18n.release_score()}
									{@render sortIcon("score")}
								</button>
							</th>
						{/if}
						<th
							scope="col"
							aria-sort={ariaSort("size")}
							class="w-24 px-3 py-2.5 text-right font-medium"
						>
							<button
								type="button"
								onclick={() => toggle("size")}
								class="touch-hit inline-flex min-w-11 items-center justify-center gap-1 uppercase tracking-[0.12em] transition hover:text-fg"
							>
								{i18n.common_size()}
								{@render sortIcon("size")}
							</button>
						</th>
						<th
							scope="col"
							aria-sort={ariaSort("seeders")}
							class="w-24 px-3 py-2.5 text-right font-medium"
						>
							<button
								type="button"
								onclick={() => toggle("seeders")}
								class="touch-hit inline-flex min-w-11 items-center justify-center gap-1 uppercase tracking-[0.12em] transition hover:text-fg"
							>
								{i18n.releases_col_seeders()}
								{@render sortIcon("seeders")}
							</button>
						</th>
						<th
							scope="col"
							class="w-28 px-3 py-2.5 text-right font-medium"
						>
							{i18n.common_action()}
						</th>
					</tr>
				</thead>
				<tbody>
					{#each rows as r (r.download_url)}
						{@const pending = pendingId === r.download_url}
						<tr
							class={cn(
								"border-b border-border last:border-b-0 transition hover:bg-surface",
								r.rejected && "opacity-60",
							)}
						>
							<td class="min-w-0 px-4 py-2.5">
								<div
									class="truncate font-mono text-[12px] text-fg"
									title={r.title}
								>
									{r.title}
								</div>
								{#if r.rejected}
									<div
										class="mt-0.5 truncate text-[11px] text-status-failed"
										title={r.reject_reason ?? i18n.release_rejected()}
									>
										{i18n.release_rejected()}{r.reject_reason
											? ` · ${r.reject_reason}`
											: ""}
									</div>
								{/if}
								{#if r.previously_grabbed_at}
									<div
										class="mt-0.5 flex items-center gap-1 text-[11px] text-status-completed"
										title={i18n.releases_seen_help()}
									>
										<History size={11} aria-hidden="true" />
										{i18n.releases_seen({ date: fmtDay(r.previously_grabbed_at) })}
									</div>
								{/if}
								<div class="mt-1 flex flex-wrap gap-1">
									{#each r.matched_formats ?? [] as f (f)}
										<span
											class="rounded-sm bg-accent-soft px-1.5 py-px font-mono text-[10px] text-accent-text"
										>
											{f}
										</span>
									{/each}
									{#if r.resolution}
										<span class="rounded-sm bg-bg-card px-1.5 py-px font-mono text-[10px] text-fg-muted">
											{r.resolution}
										</span>
									{/if}
									{#if r.source}
										<span class="rounded-sm bg-bg-card px-1.5 py-px font-mono text-[10px] text-fg-muted">
											{r.source}
										</span>
									{/if}
									{#if r.codec}
										<span class="rounded-sm bg-bg-card px-1.5 py-px font-mono text-[10px] text-fg-muted">
											{r.codec}
										</span>
									{/if}
								</div>
							</td>
							<td
								class="truncate px-3 py-2.5 font-mono text-[11.5px] text-fg"
								title={langHelp(lang(r))}
							>
								{langLabel(lang(r))}
							</td>
							<td
								class="hidden truncate px-3 py-2.5 font-mono text-[11.5px] text-fg-muted md:table-cell"
							>
								{r.release_group ?? "—"}
							</td>
							<td
								class="hidden truncate px-3 py-2.5 font-mono text-[11.5px] text-fg-muted lg:table-cell"
							>
								{r.indexer ?? "—"}
							</td>
							<td
								class="hidden whitespace-nowrap px-3 py-2.5 text-right font-mono text-[11.5px] tabular text-fg-muted sm:table-cell"
								title={fmtDate(r.published_at)}
							>
								{fmtAge(r.published_at)}
							</td>
							{#if scored}
								<td
									class={cn(
										"whitespace-nowrap px-3 py-2.5 text-right font-mono text-[11.5px] tabular font-medium",
										scoreClass(r.score ?? 0),
									)}
								>
									{r.score ?? 0}
								</td>
							{/if}
							<td
								class="whitespace-nowrap px-3 py-2.5 text-right font-mono text-[11.5px] tabular text-fg-muted"
							>
								{formatBytes(r.size)}
							</td>
							<td
								class={cn(
									"whitespace-nowrap px-3 py-2.5 text-right font-mono text-[11.5px] tabular font-medium",
									seederClass(r.seeders),
								)}
							>
								▲ {r.seeders}
							</td>
							<td class="px-3 py-2.5 text-right">
								<button
									type="button"
									onclick={() => onSelect(r)}
									disabled={pending || grab.isPending}
									class="inline-flex h-10 items-center gap-1 rounded-md bg-accent px-2.5 text-[11px] font-semibold text-fg-on-accent lg:h-7 transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
								>
									{#if pending}
										<LoaderCircle
											size={12}
											class="animate-spin"
											aria-hidden="true"
										/>
									{:else}
										<Download size={12} aria-hidden="true" />
									{/if}
									{i18n.releases_select()}
								</button>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
{/if}

<Dialog
	open={confirmFor !== null}
	title={replaceCount > 1
		? i18n.replace_confirm_title_many()
		: i18n.replace_confirm_title()}
	actions={[
		{ label: i18n.common_cancel(), variant: "ghost" },
		{
			label: i18n.replace_confirm_action(),
			variant: "danger",
			onClick: () => {
				if (confirmFor) doGrab(confirmFor, true);
			},
		},
	]}
	onClose={() => (confirmFor = null)}
>
	<p class="text-sm leading-relaxed text-fg-muted">
		{replaceCount > 1
			? i18n.replace_confirm_body_many({ count: replaceCount })
			: i18n.replace_confirm_body()}
	</p>
	{#if existing && replaceCount === 1}
		<div class="mt-3.5 rounded-md border border-border bg-bg-base px-3 py-2.5">
			<div class="font-mono text-[9.5px] uppercase tracking-[0.14em] text-fg-faint">
				{i18n.replace_current_file()}
			</div>
			{#if existing.name}
				<div class="mt-1 break-all font-mono text-[11.5px] text-fg">{existing.name}</div>
			{/if}
			<div class="mt-2 flex flex-wrap gap-1.5">
				{#if existing.lang}
					{@render langStatic(existing.lang)}
				{/if}
				{#if existing.quality}
					<span class="inline-flex h-6 whitespace-nowrap items-center rounded-sm bg-bg-card px-2 text-[12px] font-medium text-fg-muted">
						{existing.quality}
					</span>
				{/if}
				{#if existing.size}
					<span class="inline-flex h-6 whitespace-nowrap items-center rounded-sm bg-bg-card px-2 text-[12px] font-medium text-fg-muted">
						{formatBytes(existing.size)}
					</span>
				{/if}
			</div>
		</div>
	{/if}
	{#if confirmFor}
		{@const pickQuality = qualityWord(confirmFor)}
		<div class="mt-2.5 flex flex-wrap items-center gap-1.5 text-[12px] text-fg-subtle">
			<span class="mr-0.5">{i18n.replace_replaced_by()}</span>
			{@render langStatic(lang(confirmFor))}
			{#if pickQuality}
				<span class="inline-flex h-6 whitespace-nowrap items-center rounded-sm bg-bg-card px-2 text-[12px] font-medium text-fg-muted">
					{pickQuality}
				</span>
			{/if}
			<span class="inline-flex h-6 whitespace-nowrap items-center rounded-sm bg-bg-card px-2 text-[12px] font-medium text-fg-muted">
				{formatBytes(confirmFor.size)}
			</span>
		</div>
	{/if}
</Dialog>
