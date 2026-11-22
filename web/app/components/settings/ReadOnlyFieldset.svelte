<script lang="ts">
	import type { Snippet } from "svelte";
	import { twMerge } from "tailwind-merge";
	import { markConfigForm } from "@lib/config.svelte";

	let {
		children,
		class: className,
	}: { children: Snippet; class?: string } = $props();

	markConfigForm();
</script>

<fieldset class={twMerge("min-w-0", className)}>
	{@render children()}
</fieldset>
