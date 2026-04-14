<script lang="ts">
	import { createQuery, createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { slide } from "svelte/transition";
	import {
		Inbox,
		Check,
		X,
		ChevronDown,
		RotateCcw,
		Film,
		Tv,
		Star,
	} from "@lucide/svelte";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { cn } from "../../lib/cn";
	import { formatRelative } from "../../lib/dates";
	import { auth } from "../../lib/auth.svelte";
	import Dialog from "../../components/modals/Dialog.svelte";
	import Select from "../../components/forms/Select.svelte";
	import type {
		MediaRequest,
		PaginatedRequests,
		RequestCounts,
		RequestStatus,
		RequestMediaDetails,
		QualityProfile,
	} from "../../lib/types";

	type Tab = "pending" | "approved" | "rejected" | "all";
	type Kind = "all" | "movies" | "series";

	let tab = $state<Tab>("pending");
	let kind = $state<Kind>("all");
	let expandedId = $state<number | null>(null);
	// Empty = server default quality; reset whenever a new row is opened.
	let selectedProfile = $state("");

	// admin + member review (approve/deny + pick profile); request_only is read-only.
	let isReviewer = $derived(auth.canAddDirectly);

	function toggle(id: number) {
		const open = expandedId === id;
		expandedId = open ? null : id;
		selectedProfile = "";
	}

	const qc = useQueryClient();
	const requestsQuery = createQuery<PaginatedRequests>(() => ({
		queryKey: ["requests"],
		queryFn: () => api<PaginatedRequests>("/requests?page=1&limit=500"),
	}));
	const countsQuery = createQuery<RequestCounts>(() => ({
		queryKey: ["requests", "counts"],
		queryFn: () => api<RequestCounts>("/requests/counts"),
	}));
	// Cover/synopsis for the open row; refetches as expandedId changes.
	const detailQuery = createQuery<RequestMediaDetails>(() => ({
		queryKey: ["request-metadata", expandedId],
		queryFn: () => api<RequestMediaDetails>(`/requests/${expandedId}/metadata`),
		enabled: expandedId !== null,
		staleTime: 5 * 60 * 1000,
	}));
	const profilesQuery = createQuery<QualityProfile[]>(() => ({
		queryKey: ["quality-profiles"],
		queryFn: () => api<QualityProfile[]>("/quality-profiles"),
		enabled: isReviewer,
	}));

	let all = $derived(requestsQuery.data?.items ?? []);
	let counts = $derived(
		countsQuery.data ?? { pending: 0, approved: 0, denied: 0, available: 0 },
	);

	// UI "rejected" tab maps to the API "denied" status.
	function tabStatus(t: Tab): RequestStatus | null {
		if (t === "rejected") return "denied";
		if (t === "all") return null;
		return t;
	}

	let visible = $derived.by(() => {
		const st = tabStatus(tab);
		return all.filter((r) => {
			if (st && r.status !== st) return false;
			if (kind === "movies" && r.media_type !== "movie") return false;
			if (kind === "series" && r.media_type !== "tvshow") return false;
			return true;
		});
	});

	function invalidate() {
		qc.invalidateQueries({ queryKey: ["requests"] });
	}

	const approve = createMutation<
		unknown,
		Error,
		{ r: MediaRequest; profile: string }
	>(() => ({
		mutationFn: ({ r, profile }) =>
			api(`/requests/${r.id}/approve`, {
				method: "POST",
				body: { quality_profile: profile },
			}),
		onSuccess: (_d, { r }) => {
			invalidate();
			toast.ok(`Approved "${r.title}" — added to library`);
			expandedId = null;
		},
		onError: (e) => toast.err(e.message ?? "Approve failed"),
	}));

	const reopen = createMutation<unknown, Error, MediaRequest>(() => ({
		mutationFn: (r) => api(`/requests/${r.id}/reopen`, { method: "POST" }),
		onSuccess: (_d, r) => {
			invalidate();
			toast.ok(`Reopened "${r.title}"`);
		},
		onError: (e) => toast.err(e.message ?? "Reopen failed"),
	}));

	let denyTarget = $state<MediaRequest | null>(null);
	let denyReason = $state("");
	const deny = createMutation<unknown, Error, { r: MediaRequest; reason: string }>(
		() => ({
			mutationFn: ({ r, reason }) =>
				api(`/requests/${r.id}/deny`, { method: "POST", body: { reason } }),
			onSuccess: (_d, { r }) => {
				invalidate();
				toast.ok(`Rejected "${r.title}"`);
				denyTarget = null;
				denyReason = "";
				expandedId = null;
			},
			onError: (e) => toast.err(e.message ?? "Deny failed"),
		}),
	);

	function openDeny(r: MediaRequest) {
		denyTarget = r;
		denyReason = "";
	}

	const STATUS_META: Record<
		RequestStatus,
		{ label: string; token: string }
	> = {
		pending: { label: "Pending", token: "wanted" },
		approved: { label: "Approved", token: "grabbing" },
		denied: { label: "Rejected", token: "failed" },
		available: { label: "Available", token: "available" },
	};

	const tabs: { key: Tab; label: string; count?: number }[] = $derived([
		{ key: "pending", label: "Pending", count: counts.pending },
		{ key: "approved", label: "Approved", count: counts.approved },
		{ key: "rejected", label: "Rejected", count: counts.denied },
		{ key: "all", label: "All" },
	]);

	function requesterName(r: MediaRequest): string {
		return r.requester.display_name || r.requester.email;
	}
</script>

<div class="flex flex-col px-4 py-6 md:px-6">
	<header class="mb-4">
		<h1 class="text-2xl font-bold tracking-tight text-fg">Requests</h1>
		<p class="mt-1 text-sm text-fg-muted">
			{isReviewer
				? "Review and approve what your household asks for."
				: "Titles you've asked the library to add."}
		</p>
	</header>

	<!-- Live stat strip -->
	<div
		class="mb-4 grid grid-cols-2 gap-3 rounded-lg border border-border bg-bg-elevated p-4 sm:grid-cols-4"
	>
		{#each [{ n: counts.pending, l: "Pending review", hot: true }, { n: counts.approved, l: "Approved" }, { n: counts.denied, l: "Rejected" }, { n: counts.available, l: "Available" }] as s (s.l)}
			<div>
				<div
					class={cn(
						"font-mono text-2xl font-bold tabular",
						s.hot && s.n > 0 ? "text-status-wanted" : "text-fg",
					)}
				>
					{s.n}
				</div>
				<div class="mt-0.5 text-[11px] text-fg-subtle">{s.l}</div>
			</div>
		{/each}
	</div>

	<!-- Tabs + kind chips -->
	<div class="mb-4 flex flex-wrap items-center gap-3">
		<nav
			class="flex items-center gap-0.5 rounded-md border border-border bg-bg-elevated p-1"
			aria-label="Request status"
		>
			{#each tabs as t (t.key)}
				{@const active = tab === t.key}
				<button
					type="button"
					onclick={() => (tab = t.key)}
					aria-current={active ? "page" : undefined}
					class={cn(
						"inline-flex items-center gap-2 rounded-sm px-3 py-1.5 text-[12.5px] font-medium transition",
						active
							? "bg-bg-card text-fg shadow-[var(--shadow-1)]"
							: "text-fg-muted hover:text-fg",
					)}
				>
					{t.label}
					{#if t.count !== undefined}
						<span
							class={cn(
								"rounded-sm px-1.5 py-px font-mono text-[10px] tabular",
								t.key === "pending" && t.count > 0
									? "bg-status-wanted/20 text-status-wanted"
									: "bg-white/[0.04] text-fg-faint",
							)}
						>
							{t.count}
						</span>
					{/if}
				</button>
			{/each}
		</nav>

		<div
			class="flex items-center gap-0.5 rounded-md border border-border bg-bg-elevated p-1"
			role="group"
			aria-label="Media type"
		>
			{#each [{ v: "all", l: "All" }, { v: "movies", l: "Movies" }, { v: "series", l: "Series" }] as opt (opt.v)}
				<button
					type="button"
					onclick={() => (kind = opt.v as Kind)}
					aria-pressed={kind === opt.v}
					class={cn(
						"rounded-sm px-2.5 py-1 text-[11.5px] font-medium transition",
						kind === opt.v
							? "bg-bg-card text-fg shadow-[var(--shadow-1)]"
							: "text-fg-subtle hover:text-fg",
					)}
				>
					{opt.l}
				</button>
			{/each}
		</div>
	</div>

	{#if requestsQuery.isLoading}
		<p class="py-16 text-center text-sm text-fg-subtle">Loading requests…</p>
	{:else if visible.length === 0}
		<div
			class="flex flex-col items-center justify-center rounded-lg border border-dashed border-border bg-bg-card/40 py-16 text-center"
		>
			<Inbox class="mb-3 h-10 w-10 text-fg-faint" aria-hidden="true" />
			<p class="text-base font-semibold text-fg">Inbox zero</p>
			<p class="mt-1 max-w-sm text-sm text-fg-subtle">
				No {tab === "all" ? "" : tab} requests right now.
			</p>
		</div>
	{:else}
		<div class="flex flex-col gap-2">
			{#each visible as r (r.id)}
				{@const expanded = expandedId === r.id}
				{@const meta = STATUS_META[r.status]}
				<article class="overflow-hidden rounded-lg border border-border bg-bg-elevated">
					<button
						type="button"
						onclick={() => toggle(r.id)}
						aria-expanded={expanded}
						class="flex w-full items-center gap-3 px-4 py-3 text-left transition hover:bg-surface"
					>
						<span
							class="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-bg-card text-fg-muted"
						>
							{#if r.media_type === "tvshow"}
								<Tv size={16} aria-hidden="true" />
							{:else}
								<Film size={16} aria-hidden="true" />
							{/if}
						</span>
						<div class="min-w-0 flex-1">
							<div class="flex items-center gap-2">
								<span class="truncate font-medium text-fg">{r.title}</span>
								<span
									class="shrink-0 font-mono text-[10px] uppercase tracking-wide text-fg-faint"
								>
									{r.media_type === "tvshow" ? "series" : "movie"}
								</span>
							</div>
							<div class="mt-0.5 truncate text-[12px] text-fg-subtle">
								{requesterName(r)} · {formatRelative(r.created_at)}
							</div>
						</div>
						<span
							class="status-pill shrink-0 rounded-full px-2 py-0.5 text-[10.5px] font-semibold"
							style:--c={`var(--status-${meta.token})`}
						>
							{meta.label}
						</span>
						<ChevronDown
							size={16}
							class={cn(
								"shrink-0 text-fg-muted transition-transform",
								expanded && "rotate-180",
							)}
							aria-hidden="true"
						/>
					</button>

					{#if expanded}
						<div
							transition:slide={{ duration: 180 }}
							class="border-t border-border px-4 py-3"
						>
							<!-- Cover + synopsis so reviewers can judge the request -->
							<div class="mb-3 flex gap-4">
								<div
									class="aspect-[2/3] w-24 shrink-0 overflow-hidden rounded-md border border-border bg-bg-card sm:w-28"
								>
									{#if detailQuery.data?.poster_url}
										<img
											src={detailQuery.data.poster_url}
											alt={`Poster for ${r.title}`}
											loading="lazy"
											class="h-full w-full object-cover"
										/>
									{:else}
										<div class="grid h-full w-full place-items-center text-fg-faint">
											{#if r.media_type === "tvshow"}
												<Tv size={20} aria-hidden="true" />
											{:else}
												<Film size={20} aria-hidden="true" />
											{/if}
										</div>
									{/if}
								</div>
								<div class="min-w-0 flex-1">
									{#if detailQuery.isLoading}
										<div class="space-y-2" aria-hidden="true">
											<div class="h-3 w-24 animate-pulse rounded bg-white/5"></div>
											<div class="h-3 w-full animate-pulse rounded bg-white/5"></div>
											<div class="h-3 w-5/6 animate-pulse rounded bg-white/5"></div>
										</div>
									{:else if detailQuery.isError}
										<p class="text-[13px] text-fg-subtle">Couldn't load details.</p>
									{:else if detailQuery.data}
										{@const d = detailQuery.data}
										<div
											class="flex flex-wrap items-center gap-x-2 gap-y-1 font-mono text-[11px] text-fg-muted"
										>
											{#if d.year}<span class="tabular">{d.year}</span>{/if}
											{#if d.rating}
												<span class="inline-flex items-center gap-0.5">
													<Star
														size={11}
														class="text-status-wanted"
														aria-hidden="true"
													/>
													<span class="tabular">{d.rating.toFixed(1)}</span>
												</span>
											{/if}
											{#if d.runtime}<span class="tabular">{d.runtime}m</span>{/if}
											{#if d.genres?.length}
												<span class="text-fg-faint"
													>{d.genres.slice(0, 3).join(" · ")}</span
												>
											{/if}
										</div>
										{#if d.overview}
											<p
												class="mt-2 text-[13px] leading-relaxed text-fg-muted [text-wrap:pretty]"
											>
												{d.overview}
											</p>
										{:else}
											<p class="mt-2 text-[13px] text-fg-subtle">
												No synopsis available.
											</p>
										{/if}
									{/if}
								</div>
							</div>

							<dl class="grid gap-2 text-[13px] sm:grid-cols-2">
								<div>
									<dt class="text-[11px] uppercase tracking-wide text-fg-faint">
										Requested by
									</dt>
									<dd class="mt-0.5 text-fg">{r.requester.email}</dd>
								</div>
								{#if r.approved_by}
									<div>
										<dt class="text-[11px] uppercase tracking-wide text-fg-faint">
											Decided by
										</dt>
										<dd class="mt-0.5 text-fg">
											{r.approved_by.display_name || r.approved_by.email}
											· {formatRelative(r.updated_at)}
										</dd>
									</div>
								{/if}
							</dl>
							{#if r.status === "denied" && r.reason}
								<div class="mt-3">
									<div class="text-[11px] uppercase tracking-wide text-fg-faint">
										Reason
									</div>
									<blockquote
										class="mt-1 border-l-2 border-status-failed/50 pl-3 text-[13px] text-fg-muted"
									>
										{r.reason}
									</blockquote>
								</div>
							{/if}

							{#if isReviewer}
								{#if r.status === "pending"}
									<div
										class="mt-4 flex flex-wrap items-end justify-between gap-3"
									>
										<div class="min-w-[12rem]">
											<label
												for="qp-{r.id}"
												class="mb-1 block text-[11px] font-medium uppercase tracking-wide text-fg-faint"
											>
												Quality profile
											</label>
											<Select
												id="qp-{r.id}"
												value={selectedProfile}
												options={[
													{ value: "", label: "Server default" },
													...(profilesQuery.data ?? []).map((p) => ({
														value: p.name,
														label: p.name,
													})),
												]}
												onChange={(v) => (selectedProfile = v)}
											/>
										</div>
										<div class="flex items-center gap-2">
											<button
												type="button"
												onclick={() => openDeny(r)}
												class="inline-flex h-9 items-center gap-1.5 rounded-md border border-border px-3 text-sm font-medium text-fg-muted transition hover:border-border-strong hover:text-fg"
											>
												<X size={14} aria-hidden="true" />
												Reject
											</button>
											<button
												type="button"
												disabled={approve.isPending}
												onclick={() =>
													approve.mutate({ r, profile: selectedProfile })}
												class="inline-flex h-9 items-center gap-1.5 rounded-md bg-accent px-3.5 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:opacity-60"
											>
												<Check size={14} aria-hidden="true" />
												Approve &amp; add
											</button>
										</div>
									</div>
								{:else}
									<div class="mt-4 flex items-center justify-end gap-2">
										<button
											type="button"
											onclick={() => reopen.mutate(r)}
											class="inline-flex h-9 items-center gap-1.5 rounded-md border border-border px-3 text-sm font-medium text-fg-muted transition hover:border-border-strong hover:text-fg"
										>
											<RotateCcw size={14} aria-hidden="true" />
											Reopen as pending
										</button>
									</div>
								{/if}
							{/if}
						</div>
					{/if}
				</article>
			{/each}
		</div>
	{/if}
</div>

<Dialog
	open={denyTarget !== null}
	title="Reject '{denyTarget?.title ?? ''}'?"
	onClose={() => (denyTarget = null)}
	actions={[
		{ label: "Cancel", variant: "ghost", autofocus: true },
		{
			label: "Confirm rejection",
			variant: "danger",
			dismiss: false,
			pending: deny.isPending,
			onClick: () =>
				denyTarget && deny.mutate({ r: denyTarget, reason: denyReason }),
		},
	]}
>
	<label
		for="deny-reason"
		class="mb-1.5 block text-[12px] font-medium text-fg-muted"
	>
		Reason (visible to the requester)
	</label>
	<textarea
		id="deny-reason"
		bind:value={denyReason}
		rows="3"
		placeholder="e.g. Still in cinemas — let's revisit when it's on digital."
		class="w-full rounded-md border border-border bg-bg-card px-3 py-2 text-sm text-fg outline-none focus:border-accent focus:ring-2 focus:ring-accent-ring placeholder:text-fg-faint"
	></textarea>
</Dialog>

<style>
	.status-pill {
		background-color: color-mix(in srgb, var(--c) 16%, transparent);
		color: var(--c);
	}
</style>
