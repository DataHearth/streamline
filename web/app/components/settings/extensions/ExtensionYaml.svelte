<script lang="ts">
	import { Check, Copy, Lock } from "@lucide/svelte";
	import { toast } from "@lib/toast";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Where a read-only instance's extension forms end: nothing is saved from
	// here, so the entry is handed over as it goes under `extensions:` in
	// config.yaml. Same block as the Plex credentials modal.
	let { yaml, lead }: { yaml: string; lead: string } = $props();

	let copied = $state(false);
	async function copy() {
		try {
			await navigator.clipboard.writeText(yaml);
			copied = true;
			toast.ok(i18n.common_copied());
			setTimeout(() => (copied = false), 1500);
		} catch {
			toast.err(i18n.common_clipboard_unavailable());
		}
	}
</script>

<div class="flex flex-col gap-2">
	<p class="flex items-start gap-2 text-xs leading-relaxed text-status-wanted">
		<Lock size={13} class="mt-[2px] shrink-0" aria-hidden="true" />
		<span>{lead}</span>
	</p>
	<div>
		<div class="mb-1 flex items-center justify-between gap-2">
			<span class="font-mono text-[11px] font-medium text-fg-muted">config.yaml</span>
			<button
				type="button"
				onclick={copy}
				class="inline-flex min-h-11 items-center gap-1.5 rounded-md border border-border px-2.5 text-[11px] font-medium text-fg-muted transition hover:bg-surface hover:text-fg lg:h-7 lg:min-h-0"
			>
				{#if copied}
					<Check size={12} class="text-status-available" aria-hidden="true" />
					{i18n.common_copied()}
				{:else}
					<Copy size={12} aria-hidden="true" />
					{i18n.extensions_copy_yaml()}
				{/if}
			</button>
		</div>
		<pre class="overflow-x-auto rounded-md border border-border bg-bg-base p-3 font-mono text-[11px] leading-relaxed text-fg">{yaml}</pre>
	</div>
</div>
