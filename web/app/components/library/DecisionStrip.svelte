<script lang="ts">
	import { CircleCheckBig, TriangleAlert } from "@lucide/svelte";
	import Dialog from "@components/modals/Dialog.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";
	import { parts } from "@lib/message-parts";
	import { NOUN_FILE, type Noun } from "@lib/nouns";

	const skipBodyOne = parts(i18n.imports_skip_all_body_one, ["items", "skip"]);
	const skipBodyOther = parts(i18n.imports_skip_all_body_other, ["items", "skip"]);

	let {
		pendingCount,
		commitableCount,
		commitNote,
		noun = NOUN_FILE,
		skipBusy = false,
		commitBusy = false,
		onSkipAll,
		onCommit,
	}: {
		pendingCount: number;
		commitableCount: number;
		commitNote: string;
		// Row noun for a movie (files) vs series (shows) scan.
		noun?: Noun;
		skipBusy?: boolean;
		commitBusy?: boolean;
		onSkipAll: () => void;
		onCommit: () => void;
	} = $props();

	let confirmSkipOpen = $state(false);
</script>

<div
	class="mt-5 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-status-wanted/25 bg-status-wanted/10 px-4 py-3.5"
	role="status"
>
	<div class="flex min-w-0 flex-1 items-start gap-3">
		<TriangleAlert
			size={18}
			class="mt-0.5 shrink-0 text-status-wanted"
			aria-hidden="true"
		/>
		<div class="min-w-0">
			<p class="text-sm font-semibold text-fg">
				{#if pendingCount > 0}
					{pendingCount === 1
						? i18n.imports_needs_decision_one({ count: pendingCount })
						: i18n.imports_needs_decision_other({ count: pendingCount })}
				{:else}
					{commitableCount === 1
						? i18n.imports_all_ready_one({ count: commitableCount })
						: i18n.imports_all_ready_other({ count: commitableCount })}
				{/if}
			</p>
			<p class="mt-0.5 font-mono text-[10.5px] text-fg-subtle">
				{commitNote}
			</p>
		</div>
	</div>
	<div class="flex shrink-0 items-center gap-2">
		{#if pendingCount > 0}
			<button
				type="button"
				disabled={skipBusy}
				onclick={() => (confirmSkipOpen = true)}
				class="inline-flex items-center gap-1.5 rounded-md border border-border-strong bg-surface px-3.5 py-2 text-sm font-medium text-fg-muted transition hover:bg-surface-2 hover:text-fg focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring disabled:cursor-not-allowed disabled:opacity-60"
			>
				{skipBusy ? i18n.common_skipping() : i18n.imports_skip_unmatched()}
			</button>
		{/if}
		<button
			type="button"
			disabled={commitBusy || commitableCount === 0}
			onclick={onCommit}
			class="inline-flex items-center gap-1.5 rounded-md bg-accent px-4 py-2 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring disabled:cursor-not-allowed disabled:opacity-60"
		>
			<CircleCheckBig size={14} aria-hidden="true" />
			{commitBusy
				? i18n.common_starting()
				: i18n.imports_commit_items({ items: noun.count(commitableCount) })}
		</button>
	</div>
</div>

<Dialog
	open={confirmSkipOpen}
	title={i18n.imports_skip_all_title({ items: noun.items })}
	onClose={() => (confirmSkipOpen = false)}
	actions={[
		{ label: i18n.common_cancel(), variant: "ghost", autofocus: true },
		{ label: i18n.imports_skip_them(), variant: "danger", onClick: onSkipAll },
	]}
>
	<p class="text-sm text-fg-muted">
		{#each pendingCount === 1 ? skipBodyOne : skipBodyOther as p}{#if p.slot === "items"}{noun.count(pendingCount)}{:else if p.slot === "skip"}<span class="font-medium text-fg">{i18n.lc_skip()}</span>{:else}{p.text}{/if}{/each}
	</p>
</Dialog>
