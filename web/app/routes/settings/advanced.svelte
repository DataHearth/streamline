<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import {
		FolderInput,
		TriangleAlert,
		ArrowRight,
		Check,
		Info,
		Loader,
	} from "@lucide/svelte";
	import { api, errorText } from "../../lib/api";
	import { config } from "../../lib/config.svelte";
	import { toast } from "../../lib/toast";
	import { cn } from "../../lib/cn";
	import type {
		MigrationRoot,
		PathMigration,
		PathMigrationPreview,
		PathMigrationRequest,
		PathMigrationRootList,
	} from "../../lib/types";
	import Select from "../../components/forms/Select.svelte";
	import Checkbox from "../../components/forms/Checkbox.svelte";
	import Dialog from "../../components/modals/Dialog.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	const ROOTS: { value: MigrationRoot; label: string }[] = [
		{ value: "movies", label: i18n.migration_movie_library() },
		{ value: "series", label: i18n.migration_series_library() },
		{ value: "downloads", label: i18n.migration_download_folder() },
	];

	const qc = useQueryClient();

	let root = $state<MigrationRoot>("movies");
	let from = $state("");
	let to = $state("");
	let moveFiles = $state(false);
	let preview = $state<PathMigrationPreview | null>(null);
	let confirmOpen = $state(false);

	let canMove = $derived(root !== "downloads");

	const roots = createQuery<PathMigrationRootList>(() => ({
		queryKey: ["path-migration", "roots"],
		queryFn: () =>
			api<PathMigrationRootList>("/library/path-migration/roots"),
		staleTime: 30_000,
	}));

	let rootState = $derived(roots.data?.items.find((r) => r.root === root));
	let configuredRoot = $derived(rootState?.path ?? "");
	// Paths exist for this media type but none under the configured root, so
	// the server can't infer the prefix being replaced and the operator has to
	// name it. Both counts at zero is an idle download queue or an empty
	// library — normal, and nothing to migrate.
	let fromNeeded = $derived(
		rootState !== undefined &&
			rootState.tracked === 0 &&
			rootState.total > 0,
	);
	let rootEmpty = $derived(rootState !== undefined && rootState.total === 0);

	// Mirrors filepath.Clean for the shapes a path input produces: strip the
	// trailing separator so "/data/films/" and "/data/films" resolve alike.
	function clean(p: string) {
		const t = p.trim();
		return t.length > 1 ? t.replace(/\/+$/, "") : t;
	}

	// The server-side rewrite, in the browser: the prefix is swapped and
	// everything below it is carried across untouched.
	function rewrite(path: string, oldPrefix: string, newPrefix: string) {
		if (path === oldPrefix) return newPrefix;
		if (!path.startsWith(`${oldPrefix}/`)) return path;
		return newPrefix + path.slice(oldPrefix.length);
	}

	let sourcePrefix = $derived(clean(from) || configuredRoot);
	// What the configured root becomes. Unchanged when it sits outside the
	// prefix being migrated — a subtree move leaves the root where it is.
	let resultRoot = $derived(
		sourcePrefix && clean(to)
			? rewrite(configuredRoot, sourcePrefix, clean(to))
			: "",
	);
	let rootUnchanged = $derived(
		resultRoot !== "" && resultRoot === configuredRoot,
	);

	const status = createQuery<PathMigration>(() => ({
		queryKey: ["path-migration"],
		queryFn: () => api<PathMigration>("/library/path-migration"),
		refetchInterval: (q) => (q.state.data?.running ? 1000 : false),
	}));

	let live = $derived(status.data?.running === true);

	// A finished run moves the configured root and empties the old prefix, so
	// the cached root inventory is wrong the moment polling stops. Plain `let`
	// rather than $state: the effect must depend on `live` alone.
	let wasLive = false;
	$effect(() => {
		if (wasLive && !live) {
			qc.invalidateQueries({ queryKey: ["path-migration", "roots"] });
		}
		wasLive = live;
	});

	let processed = $derived(
		(status.data?.done ?? 0) + (status.data?.skipped ?? 0),
	);
	let pct = $derived(
		status.data?.total ? Math.round((processed / status.data.total) * 100) : 0,
	);

	function body(): PathMigrationRequest {
		return {
			root,
			from: from.trim() || undefined,
			to: to.trim(),
			move_files: canMove && moveFiles,
		};
	}

	const runPreview = createMutation<PathMigrationPreview, Error, void>(() => ({
		mutationFn: () =>
			api<PathMigrationPreview>("/library/path-migration/preview", {
				method: "POST",
				body: body(),
			}),
		onSuccess: (data) => (preview = data),
		onError: (err) => {
			preview = null;
			toast.err(errorText(err));
		},
	}));

	const start = createMutation<PathMigration, Error, void>(() => ({
		mutationFn: () =>
			api<PathMigration>("/library/path-migration", {
				method: "POST",
				body: body(),
			}),
		onSuccess: (data) => {
			confirmOpen = false;
			preview = null;
			status.refetch();
			toast.ok(`Migrating ${data.total} path${data.total === 1 ? "" : "s"}`);
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	// Any change to the inputs invalidates the preview: acting on stale counts
	// is exactly the mistake the preview step exists to prevent.
	function reset() {
		preview = null;
	}

	let ready = $derived(
		to.trim().length > 0 &&
			(!fromNeeded || from.trim().length > 0) &&
			!live,
	);
</script>

<div class="mx-auto max-w-4xl">
	<header>
		<h1 class="text-2xl font-bold tracking-tight text-fg">{i18n.settings_advanced()}</h1>
		<p class="mt-1 text-sm text-fg-muted">
			{i18n.settings_advanced_intro()}
		</p>
	</header>

	<section class="mt-6 rounded-lg border border-border bg-bg-card p-4">
		<div class="flex items-start gap-2.5">
			<FolderInput
				size={16}
				class="mt-0.5 shrink-0 text-fg-muted"
				aria-hidden="true"
			/>
			<div class="min-w-0">
				<h2 class="text-base font-semibold text-fg">{i18n.migration_title()}</h2>
				<p class="mt-1 text-sm text-fg-muted">
					{i18n.migration_intro()}
				</p>
			</div>
		</div>

		<div class="mt-4 flex flex-col gap-2 sm:flex-row sm:items-end sm:gap-2.5">
			<div class="shrink-0 sm:w-48">
				<Select
					label={i18n.migration_root()}
					value={root}
					options={ROOTS}
					disabled={live}
					onChange={(v) => {
						root = v;
						if (v === "downloads") moveFiles = false;
						reset();
					}}
				/>
			</div>
			<label class="min-w-0 flex-1">
				<span class="mb-1 block text-sm font-medium text-fg">
					From
					{#if fromNeeded}
						<span class="font-normal text-status-wanted">(required)</span>
					{/if}
				</span>
				<input
					type="text"
					value={fromNeeded ? from : configuredRoot}
					oninput={(e) => {
						from = e.currentTarget.value;
						reset();
					}}
					disabled={live || !fromNeeded}
					placeholder={fromNeeded ? "/media/movies" : ""}
					spellcheck="false"
					autocapitalize="off"
					autocorrect="off"
					class="w-full rounded-md border border-border bg-bg px-3 py-2 font-mono text-sm text-fg placeholder:font-sans placeholder:text-fg-faint focus:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:cursor-not-allowed disabled:opacity-60"
				/>
			</label>
			<ArrowRight
				size={15}
				class="shrink-0 rotate-90 self-center text-fg-faint sm:mb-3 sm:rotate-0 sm:self-end"
				aria-hidden="true"
			/>
			<label class="min-w-0 flex-1">
				<span class="mb-1 block text-sm font-medium text-fg">To</span>
				<input
					type="text"
					bind:value={to}
					oninput={reset}
					disabled={live}
					placeholder="/data/movies"
					spellcheck="false"
					autocapitalize="off"
					autocorrect="off"
					class="w-full rounded-md border border-border bg-bg px-3 py-2 font-mono text-sm text-fg placeholder:font-sans placeholder:text-fg-faint focus:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:cursor-not-allowed disabled:opacity-60"
				/>
			</label>
		</div>
		<div class="mt-3">
			<span class="mb-1 block text-sm font-medium text-fg">
				Resulting {ROOTS.find((r) => r.value === root)?.label.toLowerCase()} root
			</span>
			<output
				class="flex h-[38px] w-full cursor-default items-center gap-2 overflow-hidden rounded-md border border-dashed border-border bg-surface/40 px-3 font-mono text-sm"
			>
				{#if resultRoot}
					<span class="truncate text-fg-subtle">{configuredRoot}</span>
					<ArrowRight size={13} class="shrink-0 text-fg-faint" aria-hidden="true" />
					<span class="truncate font-medium text-fg">{resultRoot}</span>
					{#if rootUnchanged}
						<span class="shrink-0 font-sans text-xs text-fg-faint">
							unchanged
						</span>
					{/if}
				{:else}
					<span class="font-sans text-fg-faint">
						{i18n.migration_fill_in()} <em class="not-italic">To</em> to see where the root lands
					</span>
				{/if}
			</output>
		</div>

		<p class="mt-1.5 flex items-start gap-1.5 text-xs text-fg-muted">
			<Info size={12} class="mt-0.5 shrink-0" aria-hidden="true" />
			<span>
				{#if fromNeeded}
					Nothing is stored under <code class="font-mono text-fg-subtle">
						{configuredRoot}
					</code>, so this instance no longer knows where your files used to
					live — name that old location yourself. This is the normal state
					once you have re-pointed the root in the config, as on a read-only
					instance.
				{:else if rootEmpty}
					Nothing is stored for this root yet, so there is nothing to migrate.
				{:else}
					<em class="not-italic text-fg-subtle">{i18n.common_from()}</em> is the configured
					root, which is where your files are stored right now, so there is
					nothing to choose. It unlocks only if the config stops matching the
					database.
				{/if}
				{#if rootUnchanged}
					The root sits outside the prefix being migrated, so it stays put and
					only the matching paths move.
				{/if}
			</span>
		</p>

		{#if canMove}
			<div class="mt-3">
				<Checkbox
					checked={moveFiles}
					disabled={live}
					onChange={(v) => {
						moveFiles = v;
						reset();
					}}
					label={i18n.migration_move_files()}
					description={i18n.migration_move_files_help()}
				/>
			</div>
		{:else}
			<p
				class="mt-3 flex items-start gap-1.5 text-xs text-fg-muted"
				role="note"
			>
				<TriangleAlert size={12} class="mt-0.5 shrink-0" aria-hidden="true" />
				{i18n.migration_download_note()}
			</p>
		{/if}

		{#if config.readOnly}
			<p
				class="mt-3 flex items-start gap-1.5 rounded-md border border-status-wanted/40 bg-status-wanted/10 p-2.5 text-xs text-status-wanted"
			>
				<TriangleAlert size={12} class="mt-0.5 shrink-0" aria-hidden="true" />
				{i18n.migration_readonly_note()}
			</p>
		{/if}

		<div class="mt-4 flex flex-wrap items-center gap-2">
			<button
				type="button"
				disabled={!ready || runPreview.isPending}
				onclick={() => runPreview.mutate()}
				class="inline-flex h-9 items-center gap-1.5 rounded-md border border-border-strong bg-bg px-3 text-sm font-medium text-fg transition hover:bg-surface disabled:cursor-not-allowed disabled:opacity-60"
			>
				{runPreview.isPending ? i18n.common_checking() : i18n.common_preview()}
			</button>
			<button
				type="button"
				disabled={!preview || preview.total === 0 || live}
				onclick={() => (confirmOpen = true)}
				class="inline-flex h-9 items-center gap-1.5 rounded-md bg-status-failed px-3 text-sm font-medium text-bg-deep transition hover:bg-status-failed/90 disabled:cursor-not-allowed disabled:opacity-60"
			>
				{i18n.migration_run()}
			</button>
		</div>

		{#if preview}
			<div class="mt-4 rounded-md border border-border bg-bg p-3">
				<p class="flex flex-wrap items-center gap-1.5 text-sm text-fg">
					<span class="font-semibold">{preview.total}</span>
					path{preview.total === 1 ? "" : "s"} under
					<code class="rounded bg-surface px-1 py-0.5 font-mono text-xs">
						{preview.from}
					</code>
					<ArrowRight size={13} class="text-fg-faint" aria-hidden="true" />
					<code class="rounded bg-surface px-1 py-0.5 font-mono text-xs">
						{preview.to}
					</code>
				</p>
				{#if preview.total === 0}
					<p class="mt-1.5 text-xs text-fg-muted">
						{i18n.migration_nothing_stored()}
					</p>
				{:else if preview.skipped > 0}
					<p
						class="mt-1.5 flex items-start gap-1.5 text-xs text-status-wanted"
					>
						<TriangleAlert
							size={12}
							class="mt-0.5 shrink-0"
							aria-hidden="true"
						/>
						{preview.skipped} of them {preview.skipped === 1 ? "is" : "are"} not
						on disk where this migration expects
						{preview.skipped === 1 ? "it" : "them"}, and will be left untouched.
					</p>
				{/if}

				{#if preview.samples.length > 0}
					<ul class="mt-2.5 space-y-1 border-t border-border pt-2.5">
						{#each preview.samples as s (s.from)}
							<li class="truncate font-mono text-[11px] text-fg-subtle">
								{s.from}
								<span class="text-fg-faint">→</span>
								{s.to}
							</li>
						{/each}
					</ul>
					{#if preview.total > preview.samples.length}
						<p class="mt-1.5 text-[11px] text-fg-faint">
							and {preview.total - preview.samples.length} more
						</p>
					{/if}
				{/if}
			</div>
		{/if}

		{#if status.data && status.data.total > 0}
			{@const s = status.data}
			<div
				class="mt-4 rounded-md border border-border bg-bg p-3"
				aria-live="polite"
			>
				<div class="flex items-center justify-between gap-2">
					<p class="flex items-center gap-1.5 text-sm font-medium text-fg">
						{#if live}
							<Loader
								size={13}
								class="animate-spin text-fg-muted"
								aria-hidden="true"
							/>
							Migrating {s.root}
						{:else if s.error}
							<TriangleAlert
								size={13}
								class="text-status-failed"
								aria-hidden="true"
							/>
							Migration failed
						{:else}
							<Check
								size={13}
								class="text-status-available"
								aria-hidden="true"
							/>
							Migration finished
						{/if}
					</p>
					<span class="font-mono text-xs text-fg-subtle">
						{processed} / {s.total}
					</span>
				</div>

				<div
					class="mt-2 h-1.5 overflow-hidden rounded-full bg-surface"
					role="progressbar"
					aria-valuenow={pct}
					aria-valuemin={0}
					aria-valuemax={100}
					aria-label={i18n.migration_progress()}
				>
					<div
						class={cn(
							"h-full rounded-full transition-[width] duration-300",
							s.error ? "bg-status-failed" : "bg-accent",
						)}
						style:width="{pct}%"
					></div>
				</div>

				{#if live && s.current}
					<p class="mt-2 truncate font-mono text-[11px] text-fg-faint">
						{s.current}
					</p>
				{/if}
				{#if !live}
					<p class="mt-2 text-xs text-fg-muted">
						{s.done} re-pointed{s.skipped > 0
							? `, ${s.skipped} left untouched`
							: ""}.
					</p>
				{/if}
				{#if s.error}
					<p class="mt-1 text-xs text-status-failed">{s.error}</p>
				{/if}
			</div>
		{/if}
	</section>
</div>

<Dialog
	open={confirmOpen}
	title={i18n.migration_confirm()}
	size="lg"
	onClose={() => (confirmOpen = false)}
	actions={[
		{ label: i18n.common_cancel(), variant: "ghost" },
		{
			label: start.isPending ? i18n.common_starting() : i18n.migration_run(),
			variant: "danger",
			dismiss: false,
			pending: start.isPending,
			onClick: () => start.mutate(),
		},
	]}
>
	<p class="text-sm text-fg-muted">
		{preview?.total ?? 0} stored path{preview?.total === 1 ? "" : "s"} will be rewritten
		from
		<code class="rounded bg-surface px-1 py-0.5 font-mono text-xs">
			{preview?.from}
		</code>
		to
		<code class="rounded bg-surface px-1 py-0.5 font-mono text-xs">
			{preview?.to}
		</code>.
		{#if moveFiles}
			The files themselves will be moved.
		{/if}
	</p>
	<p class="mt-2 text-sm text-fg-muted">
		{i18n.migration_no_undo()}
	</p>
</Dialog>
