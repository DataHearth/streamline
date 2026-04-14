<script lang="ts">
	import { createQuery, createMutation } from "@tanstack/svelte-query";
	import {
		AlertTriangle,
		ChevronDown,
		ChevronUp,
		Download,
		Loader2,
	} from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import type { SearchResult } from "../../lib/types";
	import Select from "../forms/Select.svelte";

	type Field =
		| "title"
		| "size"
		| "seeders"
		| "source"
		| "codec"
		| "group"
		| "indexer"
		| "published";
	type Dir = "asc" | "desc";

	let {
		searchPath,
		grabPath,
		queryKey,
		enabled = true,
		replaceExisting = false,
		onGrabbed,
	}: {
		// POST endpoint returning ranked SearchResult[] (movie or episode browse).
		searchPath: string;
		// POST endpoint that grabs the chosen release (body is the SearchResult).
		grabPath: string;
		queryKey: readonly unknown[];
		enabled?: boolean;
		// When true, the grab body carries replace_existing so the importer
		// overwrites already-present file(s) instead of skipping them.
		replaceExisting?: boolean;
		onGrabbed?: () => void;
	} = $props();

	let sortField = $state<Field>("seeders");
	let sortDir = $state<Dir>("desc");
	let groupFilter = $state<string>("");
	let indexerFilter = $state<string>("");
	let errMsg = $state<string | null>(null);

	const q = createQuery<SearchResult[]>(() => ({
		queryKey,
		// Movie search returns a bare array; series episode search returns an
		// { items } envelope. Normalize both to SearchResult[].
		queryFn: async () => {
			const raw = await api<SearchResult[] | { items?: SearchResult[] }>(
				searchPath,
				{ method: "POST" },
			);
			return Array.isArray(raw) ? raw : (raw.items ?? []);
		},
		enabled,
		staleTime: 30_000,
	}));

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
		}
	}

	// Distinct values present in the current results, for the filter dropdowns.
	function distinct(pick: (r: SearchResult) => string | undefined): string[] {
		const set = new Set<string>();
		for (const r of q.data ?? []) {
			const v = pick(r);
			if (v) set.add(v);
		}
		return [...set].sort((a, b) => a.localeCompare(b));
	}
	let groups = $derived(distinct((r) => r.release_group));
	let indexers = $derived(distinct((r) => r.indexer));

	let groupOptions = $derived([
		{ value: "", label: "All groups" },
		...groups.map((g) => ({ value: g, label: g })),
	]);
	let indexerOptions = $derived([
		{ value: "", label: "All indexers" },
		...indexers.map((ix) => ({ value: ix, label: ix })),
	]);

	let rows = $derived.by(() => {
		const arr = (q.data ?? []).filter(
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

	const grab = createMutation<unknown, Error, SearchResult>(() => ({
		mutationFn: (r) =>
			api(grabPath, {
				method: "POST",
				body: replaceExisting ? { ...r, replace_existing: true } : r,
			}),
		onSuccess: (_d, r) => {
			toast.ok(`Grabbed: ${r.title}`);
			onGrabbed?.();
		},
		onError: (e) => {
			errMsg = e.message ?? "Grab failed";
			toast.err(errMsg);
		},
	}));

	function fmtSize(n: number): string {
		const u = ["B", "KB", "MB", "GB", "TB"];
		let i = 0;
		let v = n;
		while (v >= 1024 && i < u.length - 1) {
			v /= 1024;
			i++;
		}
		return `${v.toFixed(v < 10 ? 1 : 0)} ${u[i]}`;
	}

	// Relative age, e.g. "3h", "5d", "2mo" — the at-a-glance recency signal that
	// matters when picking a release. Absolute timestamp lives in the cell title.
	function fmtAge(iso?: string): string {
		if (!iso) return "—";
		const t = Date.parse(iso);
		if (Number.isNaN(t)) return "—";
		const s = Math.max(0, (Date.now() - t) / 1000);
		if (s < 60) return "now";
		const m = s / 60;
		if (m < 60) return `${Math.floor(m)}m`;
		const h = m / 60;
		if (h < 24) return `${Math.floor(h)}h`;
		const d = h / 24;
		if (d < 30) return `${Math.floor(d)}d`;
		const mo = d / 30;
		if (mo < 12) return `${Math.floor(mo)}mo`;
		return `${Math.floor(d / 365)}y`;
	}

	function fmtDate(iso?: string): string {
		if (!iso) return "Unknown release date";
		const t = Date.parse(iso);
		if (Number.isNaN(t)) return "Unknown release date";
		return new Date(t).toLocaleString();
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
		if (sortField === f) sortDir = sortDir === "asc" ? "desc" : "asc";
		else {
			sortField = f;
			sortDir = f === "title" ? "asc" : "desc";
		}
	}

	let pendingId = $state<string | null>(null);
	function onGrab(r: SearchResult) {
		pendingId = r.download_url;
		grab.mutate(r, {
			onSettled: () => {
				pendingId = null;
			},
		});
	}
</script>

{#snippet sortIcon(f: Field)}
	{#if sortField === f}
		{#if sortDir === "asc"}
			<ChevronUp size={12} aria-hidden="true" />
		{:else}
			<ChevronDown size={12} aria-hidden="true" />
		{/if}
	{/if}
{/snippet}

{#if errMsg}
	<div
		role="alert"
		class="mb-3 flex items-start gap-2 rounded-md border border-status-failed/40 bg-status-failed/10 p-2 text-xs text-status-failed"
	>
		<AlertTriangle class="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden="true" />
		{errMsg}
	</div>
{/if}

{#if q.isLoading}
	<ul aria-hidden="true" class="flex flex-col gap-2">
		{#each Array(6) as _}
			<li
				class="h-12 animate-pulse rounded-md bg-bg-card/50 motion-reduce:animate-none"
			></li>
		{/each}
	</ul>
{:else if q.isError}
	<div
		role="alert"
		class="rounded-lg border border-dashed border-status-failed/40 bg-status-failed/5 py-10 text-center text-sm text-status-failed"
	>
		{q.error?.message ?? "Search failed"}
	</div>
{:else if (q.data?.length ?? 0) === 0}
	<p class="py-10 text-center text-sm text-fg-muted">
		No releases found across enabled indexers.
	</p>
{:else}
	<div class="mb-3 flex flex-wrap items-center justify-between gap-3">
		<span class="tabular text-[11px] text-fg-faint">
			{rows.length} of {q.data?.length ?? 0} releases
		</span>
		<div class="flex flex-wrap items-center gap-2">
			{#if groups.length > 0}
				<div class="w-40">
					<Select
						value={groupFilter}
						options={groupOptions}
						onChange={(v) => (groupFilter = v)}
						ariaLabel="Filter by release group"
					/>
				</div>
			{/if}
			{#if indexers.length > 0}
				<div class="w-40">
					<Select
						value={indexerFilter}
						options={indexerOptions}
						onChange={(v) => (indexerFilter = v)}
						ariaLabel="Filter by indexer"
					/>
				</div>
			{/if}
		</div>
	</div>

	{#if rows.length === 0}
		<div
			class="rounded-lg border border-dashed border-border bg-bg-elevated py-10 text-center text-sm text-fg-muted"
		>
			<p>No releases match the current filters.</p>
			<button
				type="button"
				onclick={() => {
					groupFilter = "";
					indexerFilter = "";
				}}
				class="mt-2 text-xs font-medium text-accent transition hover:text-accent-hover"
			>
				Clear filters
			</button>
		</div>
	{:else}
		<div
			class="max-h-[60vh] overflow-auto rounded-lg border border-border bg-bg-elevated"
		>
			<table class="w-full min-w-[680px] table-fixed text-sm">
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
								class="inline-flex items-center gap-1 uppercase tracking-[0.12em] transition hover:text-fg"
							>
								Release
								{@render sortIcon("title")}
							</button>
						</th>
						<th
							scope="col"
							aria-sort={ariaSort("group")}
							class="hidden w-24 px-3 py-2.5 text-left font-medium md:table-cell"
						>
							<button
								type="button"
								onclick={() => toggle("group")}
								class="inline-flex items-center gap-1 uppercase tracking-[0.12em] transition hover:text-fg"
							>
								Group
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
								class="inline-flex items-center gap-1 uppercase tracking-[0.12em] transition hover:text-fg"
							>
								Indexer
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
								class="inline-flex items-center gap-1 uppercase tracking-[0.12em] transition hover:text-fg"
							>
								Released
								{@render sortIcon("published")}
							</button>
						</th>
						<th
							scope="col"
							aria-sort={ariaSort("size")}
							class="w-24 px-3 py-2.5 text-right font-medium"
						>
							<button
								type="button"
								onclick={() => toggle("size")}
								class="inline-flex items-center gap-1 uppercase tracking-[0.12em] transition hover:text-fg"
							>
								Size
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
								class="inline-flex items-center gap-1 uppercase tracking-[0.12em] transition hover:text-fg"
							>
								Seeders
								{@render sortIcon("seeders")}
							</button>
						</th>
						<th
							scope="col"
							class="w-20 px-3 py-2.5 text-right font-medium"
						>
							Action
						</th>
					</tr>
				</thead>
				<tbody>
					{#each rows as r (r.download_url)}
						{@const pending = pendingId === r.download_url}
						<tr class="border-b border-border last:border-b-0 transition hover:bg-surface">
							<td class="min-w-0 px-4 py-2.5">
								<div
									class="truncate font-mono text-[12px] text-fg"
									title={r.title}
								>
									{r.title}
								</div>
								<div class="mt-1 flex flex-wrap gap-1">
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
							<td
								class="whitespace-nowrap px-3 py-2.5 text-right font-mono text-[11.5px] tabular text-fg-muted"
							>
								{fmtSize(r.size)}
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
									onclick={() => onGrab(r)}
									disabled={pending || grab.isPending}
									class="inline-flex h-7 items-center gap-1 rounded-md bg-accent px-2.5 text-[11px] font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
								>
									{#if pending}
										<Loader2
											size={12}
											class="animate-spin"
											aria-hidden="true"
										/>
									{:else}
										<Download size={12} aria-hidden="true" />
									{/if}
									Grab
								</button>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
{/if}
