<script lang="ts">
	import Dialog from "../modals/Dialog.svelte";

	// One delete, not two. The library had a "Delete from library" and a
	// "Delete + files" entry side by side in every kebab, which made the
	// dangerous one a slip of the thumb away from the safe one. Now there is a
	// single destructive entry and the files are a checkbox inside the confirm —
	// off by default, and the confirm button relabels itself when it is on.
	let {
		open,
		title,
		body,
		filesLabel,
		filesNote,
		canDeleteFiles = true,
		pending = false,
		onClose,
		onConfirm,
	}: {
		open: boolean;
		title: string;
		body: string;
		filesLabel: string;
		filesNote?: string;
		// False when there is nothing on disk — the checkbox would be a lie.
		canDeleteFiles?: boolean;
		pending?: boolean;
		onClose: () => void;
		onConfirm: (withFiles: boolean) => void;
	} = $props();

	let withFiles = $state(false);
	// Reset on every open: a box that remembers "delete files" from the last
	// title is the one mistake this dialog exists to prevent.
	$effect(() => {
		if (open) withFiles = false;
	});
</script>

<Dialog
	{open}
	{title}
	{onClose}
	actions={[
		{ label: "Cancel", variant: "ghost", autofocus: true },
		{
			label: withFiles ? "Delete + files" : "Delete",
			variant: "danger",
			dismiss: false,
			pending,
			onClick: () => onConfirm(withFiles),
		},
	]}
>
	<p class="text-sm leading-relaxed text-fg-muted">{body}</p>
	{#if canDeleteFiles}
		<label
			class="mt-3.5 flex cursor-pointer items-start gap-2.5 rounded-lg border p-3 transition {withFiles
				? 'border-status-failed/50 bg-status-failed/10'
				: 'border-border bg-bg-card/50 hover:border-border-strong'}"
		>
			<input
				type="checkbox"
				bind:checked={withFiles}
				class="mt-0.5 h-4 w-4 shrink-0 cursor-pointer rounded accent-[var(--status-failed)]"
			/>
			<span class="min-w-0">
				<span class="block text-sm font-medium text-fg">{filesLabel}</span>
				{#if filesNote}
					<span class="mt-0.5 block text-xs text-fg-muted">{filesNote}</span>
				{/if}
			</span>
		</label>
	{/if}
</Dialog>
