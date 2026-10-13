<script lang="ts">
	import { Plus, X } from "@lucide/svelte";
	import { INPUT_CLASS } from "@lib/form";
	import { cn } from "@lib/cn";
	import { overrideRows, rowsToOverrides, type OverrideRow, type TagOverrides } from "@lib/music-tags";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Tags set by hand for an album or a track: a name and its values per row,
	// saved as a whole. A row with a name and no value removes that tag.
	let {
		value,
		disabled = false,
		pending = false,
		onSave,
	}: {
		value: TagOverrides;
		disabled?: boolean;
		pending?: boolean;
		onSave: (o: TagOverrides) => void;
	} = $props();

	// A set of overrides in one canonical spelling, keys sorted.
	const norm = (o: TagOverrides) => JSON.stringify(rowsToOverrides(overrideRows(o)));

	let rows = $state<OverrideRow[]>([]);
	// What the rows were last seeded from.
	let base = $state<TagOverrides>({});
	let typed = $derived(norm(rowsToOverrides(rows)));
	let dirty = $derived(typed !== norm(base));
	// Re-seed when the server's value moves, unless something typed would be
	// lost: untouched rows, or rows that are what was just saved, follow it.
	$effect(() => {
		const next = norm(value);
		if (next !== norm(base) && (!dirty || typed === next)) {
			rows = overrideRows(value);
			base = value;
		}
	});

	const add = () => (rows = [...rows, { key: "", values: "" }]);
	const remove = (i: number) => (rows = rows.filter((_, j) => j !== i));
</script>

<div class="flex flex-col gap-2">
	{#each rows as row, i (i)}
		<div class="flex items-center gap-2">
			<input
				class={cn(INPUT_CLASS, "w-28 shrink-0 font-mono text-xs uppercase sm:w-40")}
				placeholder={i18n.music_tag_override_key()}
				aria-label={i18n.music_tag_override_key()}
				bind:value={row.key}
				{disabled}
			/>
			<input
				class={cn(INPUT_CLASS, "min-w-0 flex-1 text-xs")}
				placeholder={i18n.music_tag_override_values()}
				aria-label={i18n.music_tag_override_values()}
				bind:value={row.values}
				{disabled}
			/>
			<button
				type="button"
				class="grid h-9 w-9 shrink-0 place-items-center rounded-md text-fg-muted transition hover:bg-white/[0.08] hover:text-fg disabled:opacity-50"
				aria-label={i18n.music_tag_override_remove()}
				title={i18n.music_tag_override_remove()}
				onclick={() => remove(i)}
				{disabled}
			>
				<X size={15} aria-hidden="true" />
			</button>
		</div>
	{/each}
	<div class="flex flex-wrap items-center gap-2">
		<button
			type="button"
			class="inline-flex h-9 items-center gap-1.5 rounded-md border border-border-strong px-3 text-[13px] text-fg transition hover:bg-white/[0.08] disabled:opacity-50"
			onclick={add}
			{disabled}
		>
			<Plus size={14} aria-hidden="true" />
			{i18n.music_tag_override_add()}
		</button>
		{#if dirty}
			<button
				type="button"
				class="inline-flex h-9 items-center rounded-md bg-accent px-3 text-[13px] font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:opacity-50"
				onclick={() => onSave(rowsToOverrides(rows))}
				disabled={disabled || pending}
			>
				{i18n.common_save()}
			</button>
			<button
				type="button"
				class="inline-flex h-9 items-center rounded-md px-3 text-[13px] text-fg-muted transition hover:text-fg"
				onclick={() => (rows = overrideRows(base))}
				disabled={pending}
			>
				{i18n.common_cancel()}
			</button>
		{/if}
	</div>
</div>
