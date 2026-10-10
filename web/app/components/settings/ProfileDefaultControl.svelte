<script lang="ts">
	import { Star } from "@lucide/svelte";
	import DropdownMenu from "@components/shared/DropdownMenu.svelte";
	import DropdownOption from "@components/shared/DropdownOption.svelte";
	import type { DefaultKind } from "@lib/profile-defaults";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The default control the three profile tabs share, in the slot the single
	// star always had. With one default (music) the star sets it. With one per
	// kind (video, books) it opens the kinds: those this profile holds are
	// ticked, picking another makes it that kind's default. One icon whatever
	// the count — four icon buttons for books would crowd a 390 row, and BD,
	// comic and manga have no glyphs a reader could tell apart. The star goes
	// once the profile holds every kind.
	let {
		name,
		kinds,
		held,
		busy = false,
		onMake,
	}: {
		name: string;
		// Empty for a single-default medium.
		kinds: DefaultKind[];
		held: string[];
		busy?: boolean;
		onMake: (kind: string | null) => void;
	} = $props();

	let open = $state(false);
	let trigger = $state<HTMLButtonElement | null>(null);
	let single = $derived(kinds.length === 0);
	let holdsAll = $derived(kinds.length === 0 ? held.length > 0 : kinds.every((k) => held.includes(k.value)));
	let label = $derived(kinds.length === 0 ? i18n.quality_make_default() : i18n.quality_make_default_named({ name }));
</script>

{#if !holdsAll}
	<button
		bind:this={trigger}
		type="button"
		disabled={busy}
		onclick={() => (single ? onMake(null) : (open = !open))}
		aria-haspopup={single ? undefined : "listbox"}
		aria-expanded={single ? undefined : open}
		aria-label={label}
		title={single ? label : i18n.quality_make_default_for()}
		class="rounded-md p-3 text-fg-muted transition hover:bg-surface hover:text-accent disabled:cursor-not-allowed disabled:opacity-60 lg:p-1.5"
	>
		<Star size={16} aria-hidden="true" />
	</button>
	{#if !single}
		<DropdownMenu
			{open}
			anchor={trigger}
			onClose={() => (open = false)}
			align="end"
			minWidth="13rem"
			ariaLabel={i18n.quality_make_default_for()}
		>
			<li role="presentation" class="px-3 pb-1 pt-1.5 font-mono text-[10px] uppercase tracking-[0.14em] text-fg-faint">
				{i18n.quality_make_default_for()}
			</li>
			{#each kinds as k (k.value)}
				{@const on = held.includes(k.value)}
				<DropdownOption
					label={k.label}
					selected={on}
					onSelect={() => {
						open = false;
						if (!on) onMake(k.value);
					}}
				/>
			{/each}
		</DropdownMenu>
	{/if}
{/if}
