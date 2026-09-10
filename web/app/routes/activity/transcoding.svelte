<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { LoaderCircle, Radar } from "@lucide/svelte";
	import { api, errorText, ApiError } from "../../lib/api";
	import { auth } from "../../lib/auth.svelte";
	import { toast } from "../../lib/toast";
	import { cn } from "../../lib/cn";
	import { fold } from "../../lib/text";
	import { pullRefresh } from "../../lib/pull-refresh";
	import { SILENT } from "../../lib/query";
	import { formatBytes } from "../../lib/format";
	import {
		TRANSCODE_SORT_CHIPS,
		anyLive,
		basename,
		sortJobs,
		totalReclaimed,
		scanWorkerUnavailable,
		transcodingDisabled,
		type TranscodeSortKey,
	} from "../../lib/transcoding";
	import type { FFmpegConfig, TranscodeJob } from "../../lib/types";
	import ActivityToolbar from "../../components/activity/ActivityToolbar.svelte";
	import ActivityFilterSheet from "../../components/activity/ActivityFilterSheet.svelte";
	import TouchStatLine from "../../components/activity/TouchStatLine.svelte";
	import TranscodeList from "../../components/activity/TranscodeList.svelte";
	import Dialog from "../../components/modals/Dialog.svelte";
	import Select from "../../components/forms/Select.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let statusFilter = $state<string[]>([]);
	let search = $state("");
	let sort = $state<TranscodeSortKey>("attention");
	let filtersOpen = $state(false);
	let expandedId = $state<number | null>(null);
	let confirmCancel = $state<TranscodeJob | null>(null);
	const qc = useQueryClient();

	// The whole list, filtered in the browser. The endpoint takes no status
	// filter: the chips are multi-select and the counts above them need every
	// state anyway — the torrents page fetches whole and filters locally for
	// the same reason.
	const jobs = createQuery<TranscodeJob[]>(() => ({
		queryKey: ["transcoding", "queue", ""],
		queryFn: () => api<TranscodeJob[]>("/transcoding/queue"),
		retry: (count, e) => !transcodingDisabled(e) && count < 1,
		// Poll only while something can still change. A restart requeues a
		// running job, so nothing is lost by stopping once everything is terminal.
		refetchInterval: (q) =>
			transcodingDisabled(q.state.error) ? false : anyLive(q.state.data) ? 2000 : false,
		refetchOnWindowFocus: (q) => !transcodingDisabled(q.state.error),
	}));

	// Shares the cache the media-probe page fills, so the missing-binary state
	// costs no request of its own. Silent: the page's own query is the one
	// allowed to raise the global activity bar.
	const ffmpeg = createQuery<FFmpegConfig>(() => ({
		queryKey: ["config", "ffmpeg"],
		queryFn: () => api<FFmpegConfig>("/config/ffmpeg"),
		enabled: auth.isAdmin,
		retry: false,
		meta: SILENT,
	}));

	function invalidate() {
		qc.invalidateQueries({ queryKey: ["transcoding"] });
	}

	const cancelJob = createMutation<null, Error, number>(() => ({
		mutationFn: (id) => api<null>(`/transcoding/jobs/${id}/cancel`, { method: "POST" }),
		onSuccess: () => {
			invalidate();
			toast.ok(i18n.transcode_canceled_toast());
		},
		onError: (e) => toast.err(errorText(e)),
	}));
	const retryJob = createMutation<null, Error, number>(() => ({
		mutationFn: (id) => api<null>(`/transcoding/jobs/${id}/retry`, { method: "POST" }),
		onSuccess: () => {
			invalidate();
			toast.ok(i18n.transcode_requeued_toast());
		},
		onError: (e) => toast.err(errorText(e)),
	}));
	// There is no progress signal for a scan — it is fire-and-forget and the
	// jobs simply appear — so the button reports that it started and stops
	// offering itself, rather than pretending to track something.
	let scanStarted = $state(false);
	const scanLibrary = createMutation<null, Error, void>(() => ({
		mutationFn: () => api<null>("/transcoding/scan", { method: "POST" }),
		onSuccess: () => {
			scanStarted = true;
			invalidate();
			toast.ok(i18n.transcode_scan_started());
		},
		onError: (e) => {
			if (scanWorkerUnavailable(e)) {
				toast.err(i18n.transcode_scan_no_ffmpeg());
				return;
			}
			if (e instanceof ApiError && e.status === 409) {
				scanStarted = true;
				toast.err(i18n.transcode_scan_running());
				return;
			}
			toast.err(errorText(e));
		},
	}));

	let allJobs = $derived<TranscodeJob[]>(jobs.data ?? []);
	let disabled = $derived(transcodingDisabled(jobs.error));
	let ffmpegMissing = $derived(
		Boolean(ffmpeg.data?.enabled) && ffmpeg.data?.found === false,
	);
	let rows = $derived.by<TranscodeJob[]>(() => {
		let out = allJobs;
		if (statusFilter.length) out = out.filter((j) => statusFilter.includes(j.status));
		if (search.trim()) {
			const q = fold(search);
			out = out.filter(
				(j) => fold(j.media_title).includes(q) || fold(j.file_path).includes(q),
			);
		}
		return sortJobs(out, sort);
	});
	let activeFilters = $derived(statusFilter.length + (search.trim() ? 1 : 0));
	let running = $derived(allJobs.filter((j) => j.status === "running").length);
	let queued = $derived(allJobs.filter((j) => j.status === "queued").length);
	let failed = $derived(
		allJobs.filter((j) => j.status === "failed" || j.status === "rejected").length,
	);
	let reclaimed = $derived(totalReclaimed(allJobs));
	let busyId = $derived.by<number | null>(() => {
		if (cancelJob.isPending) return cancelJob.variables ?? null;
		if (retryJob.isPending) return retryJob.variables ?? null;
		return null;
	});
	let headline = $derived(
		disabled
			? i18n.transcode_disabled_short()
			: [
					i18n.transcode_n_running({ n: running }),
					i18n.transcode_n_queued({ n: queued }),
					i18n.transcode_n_finished({
						n: allJobs.length - running - queued,
					}),
				].join(" · "),
	);

	function resetFilters() {
		statusFilter = [];
		search = "";
	}

	async function refreshAll() {
		if (disabled) return;
		await qc.refetchQueries({ queryKey: ["transcoding", "queue", ""] });
	}

	const headerBtn =
		"inline-flex min-h-11 items-center gap-1.5 whitespace-nowrap rounded-md border border-border px-3 text-sm font-medium text-fg-muted transition hover:bg-surface hover:text-fg disabled:cursor-not-allowed disabled:opacity-60 lg:h-9 lg:min-h-0";
</script>

<div
	use:pullRefresh={{ onRefresh: refreshAll, disabled }}
	class="group relative flex flex-col px-4 py-6 md:px-6"
>
	<div
		aria-hidden="true"
		class="pointer-events-none absolute inset-x-0 -top-11 flex h-11 items-center justify-center gap-2 text-[11.5px] text-fg-subtle opacity-0 transition-opacity group-data-[pulling]:opacity-100 md:hidden"
	>
		<LoaderCircle
			size={15}
			class="group-data-[refreshing]:motion-safe:animate-spin"
			aria-hidden="true"
		/>
		<span class="group-data-[pull-armed]:hidden group-data-[refreshing]:hidden">
			{i18n.common_pull_to_refresh()}
		</span>
		<span class="hidden group-data-[pull-armed]:inline">{i18n.common_release_to_refresh()}</span>
		<span class="hidden group-data-[refreshing]:inline">{i18n.common_refreshing()}</span>
	</div>

	<header class="mb-1 flex flex-wrap items-start justify-between gap-3">
		<div>
			<h1 class="text-2xl font-bold tracking-tight text-fg">
				{i18n.transcode_label()}
			</h1>
			<p class="mt-1 text-sm text-fg-muted">{headline}</p>
		</div>
		{#if auth.isAdmin && !disabled}
			<div class="flex flex-wrap items-center gap-2">
				<!-- Sort lives here rather than on column headers, because there are
				     no columns. Below lg the same key is set by the filter sheet's
				     chips, so the two can never disagree. The app has one dropdown
				     surface and this is it — a native <select> drops the OS menu, which
				     matches neither the row metrics nor the tinted surface every other
				     picker in the app opens. -->
				<label class="hidden items-center gap-2 lg:flex">
					<span class="font-mono text-[10px] uppercase tracking-[0.14em] text-fg-faint">
						{i18n.filter_sort()}
					</span>
					<div class="w-[190px]">
						<Select
							value={sort}
							options={TRANSCODE_SORT_CHIPS.map((o) => ({ value: o.key, label: o.label }))}
							onChange={(v) => (sort = v)}
							ariaLabel={i18n.filter_sort()}
						/>
					</div>
				</label>
				{#if scanStarted}
					<span class="font-mono text-[11px] text-fg-subtle">
						{i18n.transcode_scan_started()}
					</span>
				{:else}
					<button
						type="button"
						disabled={scanLibrary.isPending || ffmpegMissing}
						onclick={() => scanLibrary.mutate()}
						class={headerBtn}
					>
						{#if scanLibrary.isPending}
							<LoaderCircle size={14} class="motion-safe:animate-spin" aria-hidden="true" />
						{:else}
							<Radar size={14} aria-hidden="true" />
						{/if}
						{i18n.transcode_scan()}
					</button>
				{/if}
			</div>
		{/if}
	</header>

	{#if !disabled && !ffmpegMissing}
		<TouchStatLine
			stats={[
				{
					value: String(running),
					label: i18n.lc_running(),
					color: "var(--status-running)",
				},
				{ value: String(queued), label: i18n.lc_queued() },
				{
					value: formatBytes(reclaimed, "—"),
					label: i18n.transcode_reclaimed(),
					color: "var(--status-succeeded)",
				},
				{
					value: String(failed),
					label: i18n.lc_failed(),
					color: failed ? "var(--status-failed)" : undefined,
				},
			]}
		/>

		<div
			class="mb-4 mt-3 hidden grid-cols-2 gap-4 rounded-lg border border-border bg-bg-elevated px-5 py-4 sm:grid-cols-4 md:grid"
		>
			<div>
				<div class="text-2xl font-bold tabular-nums text-status-running">{running}</div>
				<div class="mt-0.5 text-[10px] font-medium uppercase tracking-[0.12em] text-fg-faint">
					{i18n.lc_running()}
				</div>
			</div>
			<div>
				<div class="text-2xl font-bold tabular-nums text-fg">{queued}</div>
				<div class="mt-0.5 text-[10px] font-medium uppercase tracking-[0.12em] text-fg-faint">
					{i18n.lc_queued()}
				</div>
			</div>
			<div>
				<div class="text-2xl font-bold tabular-nums text-status-succeeded">
					{formatBytes(reclaimed, "—")}
				</div>
				<div class="mt-0.5 text-[10px] font-medium uppercase tracking-[0.12em] text-fg-faint">
					{i18n.transcode_reclaimed()}
				</div>
			</div>
			<div>
				<div
					class={cn(
						"text-2xl font-bold tabular-nums",
						failed ? "text-status-failed" : "text-fg",
					)}
				>
					{failed}
				</div>
				<div class="mt-0.5 text-[10px] font-medium uppercase tracking-[0.12em] text-fg-faint">
					{i18n.lc_failed()}
				</div>
			</div>
		</div>

		<ActivityToolbar
			view="transcoding"
			{statusFilter}
			{search}
			{activeFilters}
			onOpenFilters={() => (filtersOpen = true)}
			onStatusFilterChange={(s) => (statusFilter = s)}
			onSearchChange={(q) => (search = q)}
		/>
	{/if}

	<TranscodeList
		{rows}
		loading={jobs.isPending && !disabled}
		error={disabled ? null : (jobs.error ?? null)}
		{disabled}
		{ffmpegMissing}
		canControl={auth.isAdmin}
		{expandedId}
		{busyId}
		onToggle={(id) => (expandedId = expandedId === id ? null : id)}
		onCancel={(job) => (confirmCancel = job)}
		onRetry={(job) => retryJob.mutate(job.id)}
		onScan={() => scanLibrary.mutate()}
	/>
</div>

<ActivityFilterSheet
	open={filtersOpen}
	onClose={() => (filtersOpen = false)}
	{search}
	onSearchChange={(q) => (search = q)}
	searchPlaceholder={i18n.transcode_filter_placeholder()}
	sortChips={TRANSCODE_SORT_CHIPS}
	sortKey={sort}
	onSortChange={(key) => (sort = key as TranscodeSortKey)}
	onReset={resetFilters}
	activeCount={activeFilters}
/>

<Dialog
	open={!!confirmCancel}
	title={i18n.transcode_cancel_confirm()}
	onClose={() => (confirmCancel = null)}
	actions={[
		{ label: i18n.common_keep(), variant: "ghost", autofocus: true },
		{
			label: i18n.transcode_cancel_job(),
			variant: "danger",
			onClick: () => confirmCancel && cancelJob.mutate(confirmCancel.id),
		},
	]}
>
	<p class="text-sm leading-relaxed text-fg-muted">
		{i18n.transcode_cancel_help()}
		<span class="font-medium text-fg">{confirmCancel?.media_title}</span>.
		<span class="mt-1.5 block font-mono text-[11px] text-fg-subtle">
			{confirmCancel ? basename(confirmCancel.file_path) : ""}
		</span>
	</p>
</Dialog>
