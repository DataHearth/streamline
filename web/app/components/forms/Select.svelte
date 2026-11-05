<script lang="ts" generics="T extends string">
	import { tick } from "svelte";
	import { ChevronDown, Search } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import { fold } from "@lib/text";
	import { m as i18n } from "@lib/paraglide/messages.js";
	import { readOnlyLock } from "@lib/config.svelte";
	import DropdownMenu from "@components/shared/DropdownMenu.svelte";
	import DropdownOption from "@components/shared/DropdownOption.svelte";
	import FieldLock from "./FieldLock.svelte";

	// hint renders as a muted second line under the option's label in the
	// dropdown — used e.g. to surface a custom format's description. It is
	// never shown in the closed (selected-value) trigger, which stays
	// label-only.
	type Option = { value: T; label: string; hint?: string };

	type Props = {
		label?: string;
		value: T;
		options: Option[];
		onChange: (v: T) => void;
		id?: string;
		disabled?: boolean;
		// Accessible name for the label-less case (e.g. a compact toolbar filter
		// where the selected value already communicates the control's purpose).
		ariaLabel?: string;
		// Opt out of the read-only lock, for a picker whose value the operator
		// needs to choose even though this instance cannot save it — the Plex
		// section pickers, whose whole purpose on a read-only instance is to let
		// someone identify which library is theirs and copy its key into config.
		// See readOnlyLock(), which already names discovery as exempt.
		readOnlyExempt?: boolean;
		// A filter field above the options, matching a label, hint or value,
		// accents and case aside. For lists long enough to hunt through.
		searchable?: boolean;
		// Caps the open list's height in px; it scrolls past that.
		maxHeight?: number;
	};

	let {
		label,
		value,
		options,
		onChange,
		id,
		disabled = false,
		ariaLabel,
		readOnlyExempt = false,
		searchable = false,
		maxHeight,
	}: Props = $props();

	const lock = readOnlyLock();
	let configLocked = $derived(!readOnlyExempt && lock());
	let off = $derived(disabled || configLocked);

	let open = $state(false);
	let triggerEl = $state<HTMLButtonElement | null>(null);
	let searchEl = $state<HTMLInputElement | null>(null);
	let query = $state("");

	let shown = $derived.by(() => {
		const q = fold(query.trim());
		if (!searchable || q === "") return options;
		return options.filter((o) =>
			[o.label, o.hint ?? "", o.value].some((t) => fold(t).includes(q)),
		);
	});

	let selectedLabel = $derived(
		options.find((o) => o.value === value)?.label ?? "",
	);

	function close() {
		open = false;
	}

	function toggle() {
		if (open) close();
		else if (!off) {
			query = "";
			open = true;
			if (searchable) void tick().then(() => searchEl?.focus());
		}
	}

	// Enter takes the first match; the down arrow moves into the list, whose
	// options are ordinary buttons from there on.
	function onSearchKey(e: KeyboardEvent) {
		if (e.key === "Enter") {
			e.preventDefault();
			if (shown[0]) pick(shown[0].value);
		} else if (e.key === "ArrowDown") {
			e.preventDefault();
			const first = (e.currentTarget as HTMLElement)
				.closest(".dd")
				?.querySelector<HTMLElement>('[role="option"]');
			first?.focus();
		}
	}

	function pick(v: T) {
		onChange(v);
		close();
		triggerEl?.focus();
	}
</script>

<div class="block">
	{#if label}
		<span class="mb-1 flex items-center gap-1.5 text-sm font-medium text-fg"
			>{label}<FieldLock locked={configLocked} /></span
		>
	{/if}
	<div class="relative">
		<button
			bind:this={triggerEl}
			{id}
			type="button"
			disabled={off}
			aria-label={label ? undefined : ariaLabel}
			aria-haspopup="listbox"
			aria-expanded={open}
			onclick={toggle}
			class={cn(
				"flex min-h-11 w-full items-center justify-between gap-2 rounded-md border border-border bg-bg px-3 text-sm text-fg transition-colors hover:border-border-strong focus-visible:outline-2 focus-visible:outline-accent lg:h-9 lg:min-h-0",
				open && "border-accent",
				off && "cursor-not-allowed opacity-60",
			)}
		>
			<span class="truncate">{selectedLabel}</span>
			<ChevronDown
				size={16}
				class={cn(
					"shrink-0 text-fg-muted transition-transform duration-150",
					open && "rotate-180",
				)}
				aria-hidden="true"
			/>
		</button>
	</div>
</div>

<DropdownMenu
	{open}
	anchor={triggerEl}
	onClose={close}
	matchAnchor
	{maxHeight}
	header={searchable ? searchField : undefined}
	ariaLabel={label ?? ariaLabel}
>
	{#each shown as o (o.value)}
		<DropdownOption
			label={o.label}
			hint={o.hint}
			title={o.hint}
			selected={value === o.value}
			onSelect={() => pick(o.value)}
		/>
	{:else}
		<li class="px-3 py-2 text-sm text-fg-muted">{i18n.common_no_matches()}</li>
	{/each}
</DropdownMenu>

{#snippet searchField()}
	<label class="flex items-center gap-2 rounded-md bg-bg px-2.5">
		<Search size={14} class="shrink-0 text-fg-muted" aria-hidden="true" />
		<input
			bind:this={searchEl}
			bind:value={query}
			type="search"
			spellcheck="false"
			autocomplete="off"
			placeholder={i18n.common_search()}
			aria-label={i18n.common_search()}
			onkeydown={onSearchKey}
			class="h-9 w-full min-w-0 bg-transparent text-sm text-fg placeholder:text-fg-faint focus:outline-none"
		/>
	</label>
{/snippet}
