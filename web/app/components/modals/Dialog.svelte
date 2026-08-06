<script lang="ts" module>
	export type DialogAction = {
		label: string;
		variant?: "primary" | "danger" | "ghost";
		// The action this button maps to. Omit for a button that only dismisses.
		onClick?: () => void;
		// Close the dialog after onClick (default true). Set false to keep it
		// open while an async action is pending and close on success instead.
		dismiss?: boolean;
		disabled?: boolean;
		pending?: boolean;
		autofocus?: boolean;
	};
</script>

<script lang="ts">
	import type { Snippet } from "svelte";
	import Modal from "./Modal.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	type Props = {
		open: boolean;
		title: string;
		// Plain-text body. For richer content pass a `children` snippet instead.
		body?: string;
		size?: "md" | "lg" | "xl" | "2xl" | "3xl" | "4xl";
		actions: DialogAction[];
		// Two buttons share a line at phone width; three or more stack. A pair is
		// always one decision with two answers, and stacking it wastes a row and
		// reads as two unrelated choices. Pass false to force the stack back.
		inlineActions?: boolean;
		onClose: () => void;
		children?: Snippet;
	};

	let {
		open,
		title,
		body,
		size = "md",
		actions,
		inlineActions,
		onClose,
		children,
	}: Props = $props();

	let inline = $derived(inlineActions ?? actions.length === 2);

	const VARIANT: Record<NonNullable<DialogAction["variant"]>, string> = {
		primary: "bg-accent text-fg-on-accent hover:bg-accent-hover",
		danger: "bg-status-failed text-bg-deep hover:bg-status-failed/90",
		ghost:
			"border border-border bg-bg-elevated text-fg hover:border-border-strong",
	};

	function run(a: DialogAction) {
		if (a.disabled || a.pending) return;
		a.onClick?.();
		if (a.dismiss !== false) onClose();
	}
</script>

<Modal {open} {title} {size} {onClose}>
	{#if children}
		{@render children()}
	{:else if body}
		<p class="text-sm leading-relaxed text-fg-muted">{body}</p>
	{/if}
	{#snippet footer()}
		{#if inline}
			<!-- One row. Modal's footer stacks below sm and forces every button to
			     w-full; inside this wrapper they share the row instead. From sm the
			     wrapper dissolves, so the grow has to be scoped too — with
			     display:contents the buttons become the footer's own flex children
			     and an unscoped flex-1 would stretch them across it. -->
			<div class="flex w-full items-center gap-2 max-sm:[&_button]:flex-1 sm:contents">
				{#each actions as a (a.label)}
					{@render action(a)}
				{/each}
			</div>
		{:else}
			{#each actions as a (a.label)}
				{@render action(a)}
			{/each}
		{/if}
	{/snippet}
</Modal>

{#snippet action(a: DialogAction)}
	<button
		type="button"
		onclick={() => run(a)}
		disabled={a.disabled || a.pending}
		data-autofocus={a.autofocus ? true : undefined}
		class="inline-flex h-9 items-center rounded-md px-3.5 text-sm font-medium transition disabled:cursor-not-allowed disabled:opacity-60 {VARIANT[
			a.variant ?? 'primary'
		]}"
	>
		{a.pending ? i18n.common_working() : a.label}
	</button>
{/snippet}
