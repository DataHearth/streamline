<script lang="ts">
	import type { Snippet } from "svelte";
	import { cn } from "@lib/cn";

	// The one container every stats block sits in: the head inside the card,
	// as on the dashboard's panels.
	let {
		title,
		caption = "",
		flush = false,
		class: cls = "",
		children,
	}: {
		title: string;
		caption?: string;
		// The body runs edge to edge (a table brings its own gutters).
		flush?: boolean;
		class?: string;
		children: Snippet;
	} = $props();
</script>

<section class={cn("min-w-0 overflow-hidden rounded-lg border border-border bg-bg-elevated", cls)}>
	<header class="flex min-w-0 items-baseline gap-3 px-4 pt-4 md:px-5 md:pt-5">
		<h3 class="shrink-0 text-[14px] font-semibold text-fg">{title}</h3>
		{#if caption}
			<span class="ml-auto min-w-0 truncate font-mono text-[11px] text-fg-subtle">{caption}</span>
		{/if}
	</header>
	<div class={flush ? "pb-2 pt-2" : "px-4 pb-4 pt-3.5 md:px-5 md:pb-5"}>
		{@render children()}
	</div>
</section>
