<script lang="ts">
	import { CircleCheckBig, TriangleAlert } from "@lucide/svelte";
	import Dialog from "../modals/Dialog.svelte";

	let {
		pendingCount,
		commitableCount,
		commitVerb,
		noun = "file",
		skipBusy = false,
		commitBusy = false,
		onSkipAll,
		onCommit,
	}: {
		pendingCount: number;
		commitableCount: number;
		commitVerb: string;
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
					{pendingCount} {noun}{pendingCount === 1 ? " needs" : "s need"} a
					decision before commit.
				{:else}
					All {commitableCount} {noun}{commitableCount === 1 ? "" : "s"} ready
					to commit.
				{/if}
			</p>
			<p class="mt-0.5 font-mono text-[10.5px] text-fg-subtle">
				Confirmed {noun}s will be {commitVerb} into the library.
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
				{skipBusy ? "Skipping…" : "Skip all unmatched"}
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
				? "Starting…"
				: `Commit ${commitableCount} ${noun}${commitableCount === 1 ? "" : "s"}`}
		</button>
	</div>
</div>

<Dialog
	open={confirmSkipOpen}
	title="Skip all unmatched {noun}s?"
	onClose={() => (confirmSkipOpen = false)}
	actions={[
		{ label: "Cancel", variant: "ghost", autofocus: true },
		{ label: "Skip them", variant: "danger", onClick: onSkipAll },
	]}
>
	<p class="text-sm text-fg-muted">
		The {pendingCount} {noun}{pendingCount === 1 ? "" : "s"} still awaiting a decision
		will be marked <span class="font-medium text-fg">skip</span> and left out of
		the import. You can still change individual files afterward.
	</p>
</Dialog>
