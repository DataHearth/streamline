<script lang="ts">
	import type { Component, Snippet } from "svelte";

	type Props = {
		icon: Component;
		title: string;
		action?: Snippet;
	};
	let { icon: Icon, title, action }: Props = $props();
</script>

<div
	class="grid place-items-center gap-2 rounded-lg border border-dashed border-border bg-bg-card/40 px-4 py-8 text-center"
>
	<Icon class="h-6 w-6 text-fg-faint" aria-hidden="true" />
	<p class="text-sm text-fg-muted">{title}</p>
	{#if action}
		{@render action()}
	{/if}
</div>
