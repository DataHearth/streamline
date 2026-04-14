<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import { Inbox } from "@lucide/svelte";
	import { api } from "../../../lib/api";
	import type { ImportScanList } from "../../../lib/types";
	import Modal from "../../../components/modals/Modal.svelte";
	import ImportsHeader from "../../../components/library/ImportsHeader.svelte";
	import ScanRow from "../../../components/library/ScanRow.svelte";
	import NewImportForm from "../../../components/library/NewImportForm.svelte";

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
			<h2 class="text-base font-semibold text-fg">Recent scans</h2>
			{#if total > 0}
				<span class="font-mono text-xs tabular-nums text-fg-subtle">
					{total}
				</span>
			{/if}
		</header>

		{#if list.isPending}
			<p class="px-5 py-10 text-center text-sm text-fg-subtle">Loading…</p>
		{:else if list.isError}
			<p class="px-5 py-10 text-center text-sm text-status-failed">
				Failed to load: {list.error?.message}
			</p>
		{:else if items.length === 0}
			<div class="flex flex-col items-center gap-2 px-5 py-12 text-center">
				<Inbox size={32} class="text-fg-faint" aria-hidden="true" />
				<p class="text-sm text-fg-muted">No scans yet.</p>
				<p class="text-xs text-fg-subtle">
					Click <span class="font-medium text-fg-muted">New scan</span>
					to start one.
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
						Prev
					</button>
					<button
						type="button"
						disabled={!hasNext}
						onclick={() => (page += 1)}
						class="inline-flex h-8 items-center rounded-md border border-border px-3 transition hover:border-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring disabled:cursor-not-allowed disabled:opacity-40"
					>
						Next
					</button>
				</div>
			</div>
		{/if}
	</section>
</div>

<Modal
	open={modalOpen}
	title="Start a new scan"
	size="lg"
	onClose={() => (modalOpen = false)}
>
	<NewImportForm onCreated={() => (modalOpen = false)} />
</Modal>
