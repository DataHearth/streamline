<script lang="ts">
	import type { AnyFieldApi } from "@tanstack/form-core";

	type Props = {
		field: AnyFieldApi;
		label: string;
		type?: "text" | "email" | "password" | "number";
		autocomplete?: string;
		placeholder?: string;
		readonly?: boolean;
		help?: string;
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
		min,
		max,
	}: Props = $props();

	let errorMessages = $derived(
		field.state.meta.errors.map((e: unknown) => {
			if (e && typeof e === "object" && "message" in e)
				return String((e as { message: unknown }).message);
			return String(e);
		}),
	);
</script>

<label class="block">
	<span class="mb-1 block text-sm font-medium text-fg">{label}</span>
	<input
		{type}
		{autocomplete}
		{placeholder}
		{readonly}
		{min}
		{max}
		inputmode={type === "number" ? "numeric" : undefined}
		name={field.name}
		value={field.state.value ?? ""}
		oninput={(e) => {
			const raw = (e.currentTarget as HTMLInputElement).value;
			field.handleChange(type === "number" ? Number(raw) : raw);
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
		<p class="mt-1 text-xs text-status-failed">{msg}</p>
	{/each}
</label>
