<script lang="ts">
	import type { AnyFieldApi } from "@tanstack/form-core";
	import type { HTMLInputAttributes } from "svelte/elements";
	import { cn } from "../../lib/cn";
	import { fieldErrorMessages } from "../../lib/fieldErrors";
	import { readOnlyLock } from "../../lib/config.svelte";
	import FieldLock from "./FieldLock.svelte";

	type Props = {
		field: AnyFieldApi;
		label: string;
		type?: "text" | "email" | "password" | "number";
		autocomplete?: HTMLInputAttributes["autocomplete"];
		placeholder?: string;
		readonly?: boolean;
		help?: string;
		// Renders validation text out of flow. For single-row forms, where a
		// field that grows by one line shoves its neighbours out of alignment.
		floatError?: boolean;
		// number-only bounds
		min?: number;
		max?: number;
	};

	let {
		field,
		label,
		type = "text",
		autocomplete,
		placeholder,
		readonly = false,
		help,
		floatError = false,
		min,
		max,
	}: Props = $props();

	let errorMessages = $derived(fieldErrorMessages(field));

	// readonly rather than disabled: an operator on a read-only instance still
	// needs to select and copy what the values currently are.
	const lock = readOnlyLock();
	let configLocked = $derived(lock());
	let locked = $derived(readonly || configLocked);
</script>

<label class={cn("block", floatError && "relative")}>
	<span class="mb-1 flex items-center gap-1.5 text-sm font-medium text-fg"
		>{label}<FieldLock locked={configLocked} /></span
	>
	<input
		{type}
		{autocomplete}
		{placeholder}
		readonly={locked}
		{min}
		{max}
		inputmode={type === "number" ? "numeric" : undefined}
		name={field.name}
		value={field.state.value ?? ""}
		oninput={(e) => {
			const raw = (e.currentTarget as HTMLInputElement).value;
			if (type !== "number") {
				field.handleChange(raw);
				return;
			}
			// Number("") is 0, which would re-render a cleared field as "0" and make
			// it impossible to clear-and-retype. undefined keeps the input empty and
			// lets the schema report the field as missing instead of saving a 0.
			field.handleChange(raw === "" ? undefined : Number(raw));
		}}
		onblur={() => field.handleBlur()}
		class="w-full rounded-md border bg-bg px-3 py-2 text-sm text-fg placeholder:text-fg-faint focus:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-60 read-only:opacity-70 read-only:cursor-not-allowed"
		class:border-status-failed={errorMessages.length > 0}
		class:border-border={errorMessages.length === 0}
	/>
	{#if help && errorMessages.length === 0}
		<p class="mt-1 text-xs text-fg-muted">{help}</p>
	{/if}
	{#each errorMessages as msg}
		<p
			class={cn(
				"mt-1 text-xs text-status-failed",
				floatError && "absolute left-0 top-full",
			)}
		>
			{msg}
		</p>
	{/each}
</label>

<style>
	/* Number fields: drop the native spin-button arrows. */
	input[type="number"]::-webkit-inner-spin-button,
	input[type="number"]::-webkit-outer-spin-button {
		-webkit-appearance: none;
		margin: 0;
	}
	input[type="number"] {
		-moz-appearance: textfield;
		appearance: textfield;
	}
</style>
