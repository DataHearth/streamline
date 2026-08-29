<script lang="ts">
	import { Replace } from "@lucide/svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let {
		checked,
		onChange,
	}: {
		checked: boolean;
		onChange: (v: boolean) => void;
	} = $props();
</script>

<!--
  Opt-in overwrite toggle for manual grabs. Amber (status-wanted) signals
  caution — the covered media's existing files are deleted and re-imported
  from this release. Deletion happens on import, not on toggle, so this stays
  cautionary rather than danger-red.
-->
<label
	title={i18n.grab_replace_help()}
	class="inline-flex min-h-11 lg:h-9 lg:min-h-0 cursor-pointer items-center gap-1.5 rounded-md border border-border bg-bg-base px-2.5 text-xs font-medium text-fg-subtle transition has-[:checked]:border-status-wanted/45 has-[:checked]:bg-status-wanted/10 has-[:checked]:text-status-wanted"
>
	<input
		type="checkbox"
		{checked}
		onchange={(e) => onChange((e.currentTarget as HTMLInputElement).checked)}
		class="sr-only"
	/>
	<Replace size={14} aria-hidden="true" />
	<span class="leading-none">{i18n.grab_replace_existing()}</span>
</label>
