<script lang="ts">
	import { CircleCheckBig, TriangleAlert } from "@lucide/svelte";
	import Dialog from "../modals/Dialog.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let {
		pendingCount,
		commitableCount,
		commitNote,
		noun = "file",
		skipBusy = false,
		commitBusy = false,
		onSkipAll,
		onCommit,
	}: {
		pendingCount: number;
		commitableCount: number;
		commitNote: string;
		// Row noun for a movie ("file") vs series ("show") scan.
		noun?: string;
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
				: commitableCount === 1 ? i18n.imports_commit_count_one({ count: commitableCount, noun }) : i18n.imports_commit_count_other({ count: commitableCount, noun })}
		</button>
	</div>
</div>

<Dialog
	open={confirmSkipOpen}
	title="Skip all unmatched {noun}s?"
	onClose={() => (confirmSkipOpen = false)}
	actions={[
		{ label: i18n.common_cancel(), variant: "ghost", autofocus: true },
		{ label: i18n.imports_skip_them(), variant: "danger", onClick: onSkipAll },
	]}
>
	<p class="text-sm text-fg-muted">
		The {pendingCount} {noun}{pendingCount === 1 ? "" : "s"} still awaiting a decision
		will be marked <span class="font-medium text-fg">skip</span> and left out of
		the import. You can still change individual files afterward.
	</p>
</Dialog>
