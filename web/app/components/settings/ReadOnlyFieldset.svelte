<script lang="ts">
	import type { Snippet } from "svelte";
	import { cn } from "../../lib/cn";
	import { markConfigForm } from "../../lib/config.svelte";

	let {
		children,
		class: className,
	}: { children: Snippet; class?: string } = $props();

	markConfigForm();
</script>

<fieldset class={cn("min-w-0", className)}>
	{@render children()}
</fieldset>
