<script lang="ts">
	import type { Snippet } from "svelte";
	import { Lock } from "@lucide/svelte";
	import { config, READONLY_HINT } from "../../lib/config.svelte";

	let { children }: { children: Snippet } = $props();
</script>

{#if config.readOnly}
	<div
		class="mb-4 flex items-center gap-2 rounded-md border border-border bg-bg-card px-3 py-2 text-xs text-fg-muted"
	>
		<Lock size={14} aria-hidden="true" />
		<span>{READONLY_HINT}</span>
	</div>
{/if}
<fieldset disabled={config.readOnly} class="min-w-0">
	{@render children()}
</fieldset>
