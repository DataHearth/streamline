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
	import Button from "@components/shared/Button.svelte";
	import type { Snippet } from "svelte";
	import Modal from "./Modal.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

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
			<!-- One row, the two buttons sharing it below sm. From sm the wrapper
			     dissolves, so the grow has to be scoped too — with display:contents
			     the buttons become the footer's own flex children and an unscoped
			     flex-1 would stretch them across it. -->
			<div class="flex w-full items-center gap-2 max-sm:[&_button]:flex-1 sm:contents">
				{#each actions as a (a.label)}
					{@render action(a)}
				{/each}
			</div>
		{:else}
			<!-- Three or more: one full-width button per line below sm, the
			     decisive one on top and Cancel nearest the thumb. A single row
			     of three never fits a phone once the labels are translated —
			     "Supprimer et rechercher à nouveau" pushed "Importer quand même"
			     off the left edge. From sm the wrapper dissolves into the
			     footer's row. -->
			<div class="flex w-full flex-col-reverse gap-2 sm:contents">
				{#each actions as a (a.label)}
					{@render action(a)}
				{/each}
			</div>
		{/if}
	{/snippet}
</Modal>

{#snippet action(a: DialogAction)}
	<Button
		variant={a.variant === "ghost" ? "secondary" : a.variant}
		type="button"
		onclick={() => run(a)}
		disabled={a.disabled || a.pending}
		data-autofocus={a.autofocus ? true : undefined}
	>
		{a.pending ? i18n.common_working() : a.label}
	</Button>
{/snippet}
