<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import { onMount } from "svelte";
	import { Inbox } from "@lucide/svelte";
	import { api, errorText } from "../../../lib/api";
	import type { ImportScanList } from "../../../lib/types";
	import Modal from "../../../components/modals/Modal.svelte";
	import ImportsHeader from "../../../components/library/ImportsHeader.svelte";
	import ScanRow from "../../../components/library/ScanRow.svelte";
	import NewImportForm from "../../../components/library/NewImportForm.svelte";
	import NewImportSheet from "../../../components/library/NewImportSheet.svelte";
	import { m as i18n } from "../../../lib/paraglide/messages.js";

	// Which container the form gets. Rendered as an either/or rather than two
	// breakpoint-hidden copies, so only one NewImportForm — and one createForm —
	// is ever mounted.
	let isTouch = $state(false);
	onMount(() => {
		const mq = window.matchMedia("(max-width: 767px)");
		const sync = () => (isTouch = mq.matches);
		sync();
		mq.addEventListener("change", sync);
		return () => mq.removeEventListener("change", sync);
	});

	const LIMIT = 20;
	let page = $state(1);
	let modalOpen = $state(false);

	const list = createQuery<ImportScanList>(() => ({
		queryKey: ["imports", { page, limit: LIMIT }],
		queryFn: () => {
			const params = new URLSearchParams({
				page: String(page),
				limit: String(LIMIT),
			});
			return api<ImportScanList>(`/library/imports?${params}`);
		},
		// Refresh while there are in-progress scans
		refetchInterval: (q) => {
			const items = q.state.data?.items ?? [];
			const live = items.some(
				(s) => s.status === "running" || s.status === "committing",
			);
			return live ? 2000 : false;
		},
	}));

	let items = $derived(list.data?.items ?? []);
	let total = $derived(list.data?.total ?? 0);
	let hasPrev = $derived(page > 1);
	let hasNext = $derived(page * LIMIT < total);

	// Counters summarise the loaded page — the panel header is literally
	// "Recent scans", so this stays honest without a status-aggregate API.
	let counts = $derived({
		running: items.filter((s) => s.status === "running").length,
		review: items.filter((s) => s.status === "awaiting_review").length,
	});
</script>

<div class="flex flex-col px-4 py-6 md:px-6">
	<ImportsHeader {counts} onNewScan={() => (modalOpen = true)} />

	<section class="mt-6 rounded-lg border border-border bg-bg-elevated">
		<header
			class="flex items-center justify-between border-b border-border px-5 py-3.5 md:px-6"
		>
			<h2 class="text-base font-semibold text-fg">{i18n.imports_recent_scans()}</h2>
			{#if total > 0}
				<span class="font-mono text-xs tabular-nums text-fg-subtle">
					{total}
				</span>
			{/if}
		</header>

		{#if list.isPending}
			<p class="px-5 py-10 text-center text-sm text-fg-subtle">{i18n.common_loading()}</p>
		{:else if list.isError}
			<p class="px-5 py-10 text-center text-sm text-status-failed">
				{i18n.err_load_failed_detail({ reason: errorText(list.error) })}
			</p>
		{:else if items.length === 0}
			<div class="flex flex-col items-center gap-2 px-5 py-12 text-center">
				<Inbox size={32} class="text-fg-faint" aria-hidden="true" />
				<p class="text-sm text-fg-muted">{i18n.imports_no_scans()}</p>
				<p class="text-xs text-fg-subtle">
					{i18n.imports_click_prefix()}
					<span class="font-medium text-fg-muted">{i18n.imports_new_scan()}</span>
					{i18n.imports_click_suffix()}
				</p>
			</div>
		{:else}
			<ul class="divide-y divide-border">
				{#each items as s (s.id)}
					<li>
						<ScanRow scan={s} />
					</li>
				{/each}
			</ul>
		{/if}

		{#if total > LIMIT}
			<div
				class="flex h-12 items-center justify-between border-t border-border px-5 text-sm text-fg-muted md:px-6"
			>
				<span class="font-mono tabular-nums">
					Page {page} · {total} total
				</span>
				<div class="flex gap-2">
					<button
						type="button"
						disabled={!hasPrev}
						onclick={() => (page = Math.max(1, page - 1))}
						class="inline-flex h-8 items-center rounded-md border border-border px-3 transition hover:border-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring disabled:cursor-not-allowed disabled:opacity-40"
					>
						{i18n.common_prev()}
					</button>
					<button
						type="button"
						disabled={!hasNext}
						onclick={() => (page += 1)}
						class="inline-flex h-8 items-center rounded-md border border-border px-3 transition hover:border-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring disabled:cursor-not-allowed disabled:opacity-40"
					>
						{i18n.common_next()}
					</button>
				</div>
			</div>
		{/if}
	</section>
</div>

{#if isTouch}
	<NewImportSheet open={modalOpen} onClose={() => (modalOpen = false)} />
{:else}
	<Modal
		open={modalOpen}
		title={i18n.imports_start_new_scan()}
		size="lg"
		onClose={() => (modalOpen = false)}
	>
		<NewImportForm onCreated={() => (modalOpen = false)} />
	</Modal>
{/if}
