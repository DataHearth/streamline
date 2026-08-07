<script lang="ts">
	import { CircleCheckBig } from "@lucide/svelte";
	import Dialog from "../modals/Dialog.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// B1: commit lives on a bar that stays with you through a long review list,
	// instead of a banner at the top of it. Sticky inside #main, so it settles
	// above the bottom nav rather than replacing it — this is not a mode.
	let {
		pendingCount,
		commitableCount,
		commitSummary,
		series = false,
		skipBusy = false,
		commitBusy = false,
		onSkipAll,
		onCommit,
	}: {
		pendingCount: number;
		commitableCount: number;
		commitSummary: string;
		series?: boolean;
		skipBusy?: boolean;
		commitBusy?: boolean;
		onSkipAll: () => void;
		onCommit: () => void;
	} = $props();

	let confirmSkipOpen = $state(false);

	const prefix = $derived(
		series
			? i18n.imports_skip_body_prefix_show
			: i18n.imports_skip_body_prefix_file,
	);
</script>

<div
	class="sticky bottom-0 z-20 -mx-4 mt-4 flex items-center gap-3 border-t border-border bg-bg-elevated/95 px-4 py-2.5 backdrop-blur-md md:-mx-6 md:px-6 lg:hidden"
	role="status"
>
	<div class="min-w-0 flex-1">
		<p class="truncate text-[13px] font-semibold tracking-[-0.01em] text-fg">
			{i18n.imports_ready_to_commit({ count: commitableCount })}
		</p>
		<p class="mt-0.5 truncate font-mono text-[10.5px] text-fg-subtle">
			{#if pendingCount > 0}
				{i18n.imports_still_need_decision({ count: pendingCount })}
			{:else}
				{commitSummary}
			{/if}
		</p>
	</div>

	{#if pendingCount > 0}
		<button
			type="button"
			disabled={skipBusy}
			onclick={() => (confirmSkipOpen = true)}
			class="inline-flex h-10 shrink-0 items-center rounded-xl border border-border bg-surface px-3 text-[13px] font-medium text-fg-muted transition active:bg-surface-2 disabled:opacity-60"
		>
			{skipBusy ? i18n.common_skipping() : i18n.imports_skip_all()}
		</button>
	{/if}
	<button
		type="button"
		disabled={commitBusy || commitableCount === 0}
		onclick={onCommit}
		class="inline-flex h-10 shrink-0 items-center gap-1.5 rounded-xl bg-accent px-4 text-[13px] font-semibold text-fg-on-accent transition active:bg-accent-pressed disabled:cursor-not-allowed disabled:opacity-60"
	>
		<CircleCheckBig size={14} aria-hidden="true" />
		{commitBusy
			? i18n.common_starting()
			: i18n.imports_commit_n({ count: commitableCount })}
	</button>
</div>

<Dialog
	open={confirmSkipOpen}
	title={series
		? i18n.imports_skip_all_unmatched_shows()
		: i18n.imports_skip_all_unmatched_files()}
	onClose={() => (confirmSkipOpen = false)}
	actions={[
		{ label: i18n.common_cancel(), variant: "ghost", autofocus: true },
		{ label: i18n.imports_skip_them(), variant: "danger", onClick: onSkipAll },
	]}
>
	<p class="text-sm text-fg-muted">
		{prefix({ count: pendingCount })}
		<span class="font-medium text-fg">{i18n.lc_skip()}</span>
		{series ? i18n.imports_skip_body_suffix_show() : i18n.imports_skip_body_suffix_file()}
	</p>
</Dialog>
