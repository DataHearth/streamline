<script lang="ts" generics="T extends string">
	import { ChevronDown } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { readOnlyLock } from "../../lib/config.svelte";
	import DropdownMenu from "../shared/DropdownMenu.svelte";
	import DropdownOption from "../shared/DropdownOption.svelte";
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
	}: Props = $props();

	const lock = readOnlyLock();
	let configLocked = $derived(!readOnlyExempt && lock());
	let off = $derived(disabled || configLocked);

	let open = $state(false);
	let triggerEl = $state<HTMLButtonElement | null>(null);

	let selectedLabel = $derived(
		options.find((o) => o.value === value)?.label ?? "",
	);

	function close() {
		open = false;
	}

	function toggle() {
		if (open) close();
		else if (!off) open = true;
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
	ariaLabel={label ?? ariaLabel}
>
	{#each options as o (o.value)}
		<DropdownOption
			label={o.label}
			hint={o.hint}
			title={o.hint}
			selected={value === o.value}
			onSelect={() => pick(o.value)}
		/>
	{/each}
</DropdownMenu>
