<script lang="ts">
	import { Plus, X } from "@lucide/svelte";
	import { readOnlyLock } from "@lib/config.svelte";
	import { INPUT_CLASS } from "@lib/form";
	import FieldLock from "@components/forms/FieldLock.svelte";
	import { pathMappingIssue } from "@lib/schemas";
	import type { MediaServerPathMapping as PathMapping } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	type Props = {
		value: PathMapping[];
		onChange: (v: PathMapping[]) => void;
	};

	let { value, onChange }: Props = $props();

	const lock = readOnlyLock();
	let locked = $derived(lock());

	function set(i: number, side: keyof PathMapping, v: string) {
		onChange(value.map((m, j) => (j === i ? { ...m, [side]: v } : m)));
	}
</script>

<div class="space-y-2">
	<span class="flex items-center gap-1.5 text-sm font-medium text-fg">
		{i18n.mediaserver_path_mappings()}<FieldLock locked={locked} />
	</span>
	<p class="text-xs text-fg-muted">{i18n.mediaserver_path_mappings_help()}</p>
	{#each value as m, i (i)}
		<div class="space-y-1">
			<div class="flex items-center gap-2">
				<input
					type="text"
					aria-label={i18n.mediaserver_path_from()}
					placeholder="/srv/streamline/movies"
					value={m.from}
					readonly={locked}
					oninput={(e) => set(i, "from", e.currentTarget.value)}
					class="{INPUT_CLASS} min-w-0 flex-1 font-mono"
				/>
				<span class="shrink-0 text-fg-faint" aria-hidden="true">→</span>
				<input
					type="text"
					aria-label={i18n.mediaserver_path_to()}
					placeholder="/data/movies"
					value={m.to}
					readonly={locked}
					oninput={(e) => set(i, "to", e.currentTarget.value)}
					class="{INPUT_CLASS} min-w-0 flex-1 font-mono"
				/>
				{#if !locked}
					<button
						type="button"
						onclick={() => onChange(value.filter((_, j) => j !== i))}
						class="grid h-9 w-9 shrink-0 place-items-center rounded-md text-fg-muted transition hover:bg-status-failed/10 hover:text-status-failed"
						aria-label={i18n.mediaserver_path_remove()}
					>
						<X size={14} aria-hidden="true" />
					</button>
				{/if}
			</div>
			{#if pathMappingIssue(m)}
				<p class="text-xs text-status-failed">{pathMappingIssue(m)}</p>
			{/if}
		</div>
	{/each}
	{#if !locked}
		<button
			type="button"
			onclick={() => onChange([...value, { from: "", to: "" }])}
			class="inline-flex h-8 items-center gap-1.5 rounded-md border border-border-strong bg-surface px-3 text-xs font-medium text-fg-muted transition hover:bg-surface-2 hover:text-fg"
		>
			<Plus size={13} aria-hidden="true" />
			{i18n.mediaserver_path_add()}
		</button>
	{/if}
</div>
