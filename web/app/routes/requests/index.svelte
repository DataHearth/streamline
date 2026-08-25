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
	} from "@lucide/svelte";
	import { api, apiAllPages, type Paginated } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { cn } from "../../lib/cn";
	import { formatRelative } from "../../lib/dates";
	import { auth } from "../../lib/auth.svelte";
	import Dialog from "../../components/modals/Dialog.svelte";
	import Select from "../../components/forms/Select.svelte";
	import LookupDetailPanel from "../../components/shared/LookupDetailPanel.svelte";
	import RequestStatLine from "../../components/requests/RequestStatLine.svelte";
	import RequestFilterLine from "../../components/requests/RequestFilterLine.svelte";
	import RequestFilterSheet from "../../components/requests/RequestFilterSheet.svelte";
	import RequestTouchList from "../../components/requests/RequestTouchList.svelte";
	import RequestDecisionSheet from "../../components/requests/RequestDecisionSheet.svelte";
	import MyRequestsList from "../../components/requests/MyRequestsList.svelte";
	import {
		STATUS_META,
		activeFilterCount,
		filterRequests,
		requesterName,
		statusChips,
		type RequestKind,
		type RequestTab,
	} from "../../lib/requests-touch";
	import type {
		MediaRequest,
		RequestCounts,
		RequestMediaDetails,
		QualityProfile,
	} from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let tab = $state<RequestTab>("pending");
	// The two bands land on different states, so they cannot share one value. The
	// lg tab bar opens on Pending because it can say so on screen; the touch list
	// has no status control at all and opens on the whole list, sectioned.
	let touchTab = $state<RequestTab>("all");
	let kind = $state<RequestKind>("all");
	let query = $state("");
	let filterOpen = $state(false);
	// The accordion (lg) and the sheet (below lg) are two containers for the same
	// decision, so each tracks its own open row — but both by id, never by the
	// object: a refetch replaces the items and a held one would show a stale
	// decision.
	let expandedId = $state<number | null>(null);
	let sheetId = $state<number | null>(null);
	// Empty = server default quality; reset whenever a new row is opened.
	let selectedProfile = $state("");

	// admin + member review (approve/deny + pick profile); request_only is read-only.
	let isReviewer = $derived(auth.canAddDirectly);

	function startProfile(id: number): string {
		// Start the reviewer on whatever the requester asked for; still overridable.
		return all.find((r) => r.id === id)?.quality_profile ?? "";
	}

	function toggle(id: number) {
		const open = expandedId === id;
		expandedId = open ? null : id;
		selectedProfile = open ? "" : startProfile(id);
	}

	function openSheet(r: MediaRequest) {
		sheetId = r.id;
		selectedProfile = r.quality_profile ?? "";
	}

	const qc = useQueryClient();
	const requestsQuery = createQuery<Paginated<MediaRequest>>(() => ({
		queryKey: ["requests"],
		queryFn: () => apiAllPages<MediaRequest>("/requests"),
	}));
	const countsQuery = createQuery<RequestCounts>(() => ({
		queryKey: ["requests", "counts"],
		queryFn: () => api<RequestCounts>("/requests/counts"),
	}));
	// Cover/synopsis for whichever row is open — expanded on desktop, or the
	// sheet below lg. One query serves both; only one can be open at a time.
	let detailId = $derived(sheetId ?? expandedId);
	const detailQuery = createQuery<RequestMediaDetails>(() => ({
		queryKey: ["request-metadata", detailId],
		queryFn: () => api<RequestMediaDetails>(`/requests/${detailId}/metadata`),
		enabled: detailId !== null,
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

	let visible = $derived(filterRequests(all, { tab, kind, query }));
	let touchVisible = $derived(filterRequests(all, { tab: touchTab, kind, query }));
	let activeFilters = $derived(activeFilterCount({ tab: touchTab, kind }));
	let sheetRequest = $derived(all.find((r) => r.id === sheetId) ?? null);
	let tabs = $derived(statusChips(counts));

	function resetFilters() {
		touchTab = "all";
		kind = "all";
	}

	function invalidate() {
		qc.invalidateQueries({ queryKey: ["requests"] });
	}

	// Approving is the only decision that writes to the library — it adds the
	// movie/show — so the grids and the sidebar counts go stale with it. Deny
	// and reopen only move the request's own status.
	function invalidateLibrary(mediaType: MediaRequest["media_type"]) {
		const root = mediaType === "tvshow" ? "series" : "movies";
		qc.invalidateQueries({ queryKey: [root] });
		qc.invalidateQueries({ queryKey: [root, "counts"] });
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
			invalidateLibrary(r.media_type);
			toast.ok(i18n.requests_approved_toast({ title: r.title }));
			expandedId = null;
			sheetId = null;
		},
		onError: (e) => toast.err(e.message ?? i18n.requests_approve_failed()),
	}));

	const reopen = createMutation<unknown, Error, MediaRequest>(() => ({
		mutationFn: (r) => api(`/requests/${r.id}/reopen`, { method: "POST" }),
		onSuccess: (_d, r) => {
			invalidate();
			toast.ok(i18n.requests_reopened_toast({ title: r.title }));
			sheetId = null;
		},
		onError: (e) => toast.err(e.message ?? i18n.requests_reopen_failed()),
	}));

	let denyTarget = $state<MediaRequest | null>(null);
	let denyReason = $state("");
	const deny = createMutation<unknown, Error, { r: MediaRequest; reason: string }>(
		() => ({
			mutationFn: ({ r, reason }) =>
				api(`/requests/${r.id}/deny`, { method: "POST", body: { reason } }),
			onSuccess: (_d, { r }) => {
				invalidate();
				toast.ok(i18n.requests_rejected_toast({ title: r.title }));
				denyTarget = null;
				denyReason = "";
				expandedId = null;
			},
			onError: (e) => toast.err(e.message ?? i18n.requests_deny_failed()),
		}),
	);

	function openDeny(r: MediaRequest) {
		denyTarget = r;
		denyReason = "";
	}

	// Rejecting wants a reason, so the sheet hands off to the dialog rather than
	// stacking one modal surface on another.
	function rejectFromSheet(r: MediaRequest) {
		sheetId = null;
		openDeny(r);
	}
</script>

<div class="flex flex-col px-4 py-6 md:px-6">
	<header class="mb-4">
		<h1 class="text-2xl font-bold tracking-tight text-fg">{i18n.requests_label()}</h1>
		<p class="mt-1 text-sm text-fg-muted">
			{isReviewer
				? i18n.requests_intro()
				: i18n.requests_intro_user()}
		</p>
	</header>

	<!-- Live stat strip. Below md it is RequestStatLine's single line instead —
	     four tiles is 96px of numbers the filter sheet's chips already carry. A
	     requester gets neither: their grouped list counts its own sections. -->
	<div
		class={cn(
			"mb-4 hidden grid-cols-4 gap-3 rounded-lg border border-border bg-bg-elevated p-4",
			isReviewer ? "md:grid" : "lg:grid",
		)}
	>
		{#each [{ n: counts.pending, l: i18n.requests_pending_review(), cls: "text-status-wanted" }, { n: counts.approved, l: i18n.common_approved(), cls: "text-status-grabbing" }, { n: counts.denied, l: i18n.common_rejected(), cls: "text-status-failed" }, { n: counts.available, l: i18n.status_available(), cls: "text-status-available" }] as s (s.l)}
			<div>
				<div
					class={cn(
						"font-mono text-2xl font-bold tabular",
						s.n > 0 ? s.cls : "text-fg-faint",
					)}
				>
					{s.n}
				</div>
				<div class="mt-0.5 text-[11px] text-fg-subtle">{s.l}</div>
			</div>
		{/each}
	</div>

	{#if isReviewer}
		<RequestStatLine {counts} />
		<RequestFilterLine
			{query}
			onQueryChange={(q) => (query = q)}
			activeCount={activeFilters}
			onOpenFilter={() => (filterOpen = true)}
		/>
	{/if}

	<!-- Tabs + kind chips. From lg up only: four labelled tabs with count badges
	     and a three-cell group cannot share 358px, and below that the search line
	     and the filter sheet own the same two pieces of state. -->
	<div class="mb-4 hidden flex-wrap items-center gap-3 lg:flex">
		<nav
			class="flex items-center gap-0.5 rounded-md border border-border bg-bg-elevated p-1"
			aria-label={i18n.requests_status()}
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
			aria-label={i18n.imports_media_type()}
		>
			{#each [{ v: "all", l: i18n.common_all() }, { v: "movies", l: i18n.movies_label() }, { v: "series", l: i18n.series_label() }] as opt (opt.v)}
				<button
					type="button"
					onclick={() => (kind = opt.v as RequestKind)}
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
		<p class="py-16 text-center text-sm text-fg-subtle">{i18n.common_loading_requests()}</p>
	{:else}
		{#if !isReviewer}
			<MyRequestsList requests={all} onOpen={openSheet} />
		{:else}
			<RequestTouchList
				requests={touchVisible}
				busy={approve.isPending}
				onOpen={openSheet}
				onApprove={(req) =>
					approve.mutate({ r: req, profile: req.quality_profile ?? "" })}
				onReject={openDeny}
			/>
		{/if}

		{#if visible.length === 0}
			<div
				class="hidden flex-col items-center justify-center rounded-lg border border-dashed border-border bg-bg-card/40 py-16 text-center lg:flex"
			>
				<Inbox class="mb-3 h-10 w-10 text-fg-faint" aria-hidden="true" />
				<p class="text-base font-semibold text-fg">{i18n.requests_inbox_zero()}</p>
				<p class="mt-1 max-w-sm text-sm text-fg-subtle">
					No {tab === "all" ? "" : tab} requests right now.
				</p>
			</div>
		{:else}
			<div class="hidden flex-col gap-2 lg:flex">
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
								<!-- Cover, synopsis, cast and IDs so reviewers can judge the request -->
								<div class="mb-3">
									{#if detailQuery.isError}
										<p class="text-[13px] text-fg-subtle">{i18n.requests_load_failed()}</p>
									{:else}
										<LookupDetailPanel
											kind={r.media_type === "tvshow" ? "series" : "movie"}
											item={{
												title: r.title,
												year: detailQuery.data?.year,
												poster_url: detailQuery.data?.poster_url,
												overview: detailQuery.data?.overview,
											}}
											detail={detailQuery.data}
											loading={detailQuery.isLoading}
											showTitle={false}
											compact
										/>
									{/if}
								</div>

								<dl class="grid gap-2 text-[13px] sm:grid-cols-2">
									<div>
										<dt class="text-[11px] uppercase tracking-wide text-fg-faint">
											{i18n.requests_requested_by()}
										</dt>
										<dd class="mt-0.5 text-fg">{r.requester.email}</dd>
									</div>
									<div>
										<dt class="text-[11px] uppercase tracking-wide text-fg-faint">
											{i18n.quality_preferred()}
										</dt>
										<dd class="mt-0.5 font-mono text-fg">
											{r.quality_profile || i18n.quality_no_preference()}
										</dd>
									</div>
									{#if r.approved_by}
										<div>
											<dt class="text-[11px] uppercase tracking-wide text-fg-faint">
												{i18n.requests_decided_by()}
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
											{i18n.common_reason()}
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
													{i18n.quality_profile()}
												</label>
												<Select
													id="qp-{r.id}"
													value={selectedProfile}
													options={[
														{ value: "", label: i18n.quality_server_default() },
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
													{i18n.common_reject()}
												</button>
												<button
													type="button"
													disabled={approve.isPending}
													onclick={() =>
														approve.mutate({ r, profile: selectedProfile })}
													class="inline-flex h-9 items-center gap-1.5 rounded-md bg-accent px-3.5 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:opacity-60"
												>
													<Check size={14} aria-hidden="true" />
													{i18n.requests_approve_add()}
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
												{i18n.requests_reopen()}
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
	{/if}
</div>

<RequestDecisionSheet
	request={sheetRequest}
	detail={detailQuery.data}
	detailLoading={detailQuery.isLoading}
	detailError={detailQuery.isError}
	reviewer={isReviewer}
	profiles={profilesQuery.data ?? []}
	profile={selectedProfile}
	onProfileChange={(v) => (selectedProfile = v)}
	busy={approve.isPending || reopen.isPending}
	onClose={() => (sheetId = null)}
	onApprove={(r, profile) => approve.mutate({ r, profile })}
	onReject={rejectFromSheet}
	onReopen={(r) => reopen.mutate(r)}
/>

<RequestFilterSheet
	open={filterOpen}
	onClose={() => (filterOpen = false)}
	tab={touchTab}
	onTabChange={(t) => (touchTab = t)}
	{kind}
	onKindChange={(k) => (kind = k)}
	{counts}
	resultCount={touchVisible.length}
	activeCount={activeFilters}
	onReset={resetFilters}
/>

<Dialog
	open={denyTarget !== null}
	title={i18n.requests_reject_confirm_title({ title: denyTarget?.title ?? "" })}
	onClose={() => (denyTarget = null)}
	actions={[
		{ label: i18n.common_cancel(), variant: "ghost", autofocus: true },
		{
			label: i18n.requests_confirm_rejection(),
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
		{i18n.requests_reason_visible()}
	</label>
	<textarea
		id="deny-reason"
		bind:value={denyReason}
		rows="3"
		placeholder={i18n.requests_reason_example()}
		class="w-full rounded-md border border-border bg-bg-card px-3 py-2 text-sm text-fg outline-none focus:border-accent focus:ring-2 focus:ring-accent-ring placeholder:text-fg-faint"
	></textarea>
</Dialog>

<style>
	.status-pill {
		background-color: color-mix(in srgb, var(--c) 16%, transparent);
		color: var(--c);
	}
</style>
