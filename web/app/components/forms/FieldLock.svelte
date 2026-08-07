<script lang="ts">
	import { Lock } from "@lucide/svelte";
	import { READONLY_HINT } from "../../lib/config.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// The marker that says *this control* is the locked one. Before it, a locked
	// field was cursor-not-allowed plus reduced opacity: a cursor state a touch
	// device cannot show, and an opacity drop that reads as "disabled until you
	// fill in something else" rather than "managed outside the app".
	let { locked }: { locked: boolean } = $props();
</script>

{#if locked}
	<span
		class="inline-flex shrink-0 items-center text-status-wanted"
		title={READONLY_HINT}
	>
		<Lock size={11} aria-hidden="true" />
		<span class="sr-only">{i18n.settings_readonly_config()}</span>
	</span>
{/if}
