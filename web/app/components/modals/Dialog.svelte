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

	type Props = {
		open: boolean;
		title: string;
		// Plain-text body. For richer content pass a `children` snippet instead.
		body?: string;
		size?: "md" | "lg" | "xl" | "2xl" | "3xl" | "4xl";
		actions: DialogAction[];
		onClose: () => void;
		children?: Snippet;
	};

	let { open, title, body, size = "md", actions, onClose, children }: Props =
		$props();

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
		{#each actions as a (a.label)}
			<button
				type="button"
				onclick={() => run(a)}
				disabled={a.disabled || a.pending}
				data-autofocus={a.autofocus ? true : undefined}
				class="inline-flex h-9 items-center rounded-md px-3.5 text-sm font-medium transition disabled:cursor-not-allowed disabled:opacity-60 {VARIANT[
					a.variant ?? 'primary'
				]}"
			>
				{a.pending ? "Working…" : a.label}
			</button>
		{/each}
	{/snippet}
</Modal>
