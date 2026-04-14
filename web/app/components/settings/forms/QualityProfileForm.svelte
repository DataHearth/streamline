<script lang="ts">
	import type { FormApi } from "@tanstack/svelte-form";
	import TextField from "../../forms/TextField.svelte";
	import Select from "../../forms/Select.svelte";
	import Checkbox from "../../forms/Checkbox.svelte";
	import type { Resolution } from "../../../lib/types";

	type Values = {
		name: string;
		preferred_resolution: Resolution;
		min_resolution: Resolution;
		upgrade_allowed: boolean;
	};

	type Props = { form: FormApi<Values, undefined> };
	let { form }: Props = $props();

	const RESOLUTIONS: Resolution[] = ["720p", "1080p", "2160p"];
</script>

<div class="space-y-4">
	<form.Field name="name">
		{#snippet children(field)}
			<TextField {field} label="Name" placeholder="1080p preferred" />
		{/snippet}
	</form.Field>

	<div class="grid gap-3 sm:grid-cols-2">
		<form.Field name="preferred_resolution">
			{#snippet children(field)}
				<div>
					<Select
						label="Preferred resolution"
						value={field.state.value}
						options={RESOLUTIONS.map((r) => ({ value: r, label: r }))}
						onChange={(v) => field.handleChange(v)}
					/>
					<p class="mt-1 text-xs text-fg-muted">
						Releases at this resolution score highest.
					</p>
				</div>
			{/snippet}
		</form.Field>

		<form.Field name="min_resolution">
			{#snippet children(field)}
				<div>
					<Select
						label="Minimum resolution"
						value={field.state.value}
						options={RESOLUTIONS.map((r) => ({ value: r, label: r }))}
						onChange={(v) => field.handleChange(v)}
					/>
					<p class="mt-1 text-xs text-fg-muted">
						Releases below this resolution are skipped.
					</p>
				</div>
			{/snippet}
		</form.Field>
	</div>

	<form.Field name="upgrade_allowed">
		{#snippet children(field)}
			<Checkbox
				name={field.name}
				checked={field.state.value}
				onChange={(v) => field.handleChange(v)}
				label="Allow upgrades"
				description="Re-grab when a higher-quality release becomes available."
			/>
		{/snippet}
	</form.Field>
</div>
