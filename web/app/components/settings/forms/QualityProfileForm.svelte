<script lang="ts">
	import type { FormApi } from "@tanstack/svelte-form";
	import TextField from "../../forms/TextField.svelte";
	import Select from "../../forms/Select.svelte";
	import Checkbox from "../../forms/Checkbox.svelte";
	import type { Resolution } from "../../../lib/types";
	import { m as i18n } from "../../../lib/paraglide/messages.js";

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
			<TextField {field} label={i18n.common_name()} placeholder="1080p preferred" />
		{/snippet}
	</form.Field>

	<div class="grid gap-3 sm:grid-cols-2">
		<form.Field name="preferred_resolution">
			{#snippet children(field)}
				<div>
					<Select
						label={i18n.quality_preferred_resolution()}
						value={field.state.value}
						options={RESOLUTIONS.map((r) => ({ value: r, label: r }))}
						onChange={(v) => field.handleChange(v)}
					/>
					<p class="mt-1 text-xs text-fg-muted">
						{i18n.quality_preferred_help()}
					</p>
				</div>
			{/snippet}
		</form.Field>

		<form.Field name="min_resolution">
			{#snippet children(field)}
				<div>
					<Select
						label={i18n.quality_minimum_resolution()}
						value={field.state.value}
						options={RESOLUTIONS.map((r) => ({ value: r, label: r }))}
						onChange={(v) => field.handleChange(v)}
					/>
					<p class="mt-1 text-xs text-fg-muted">
						{i18n.quality_minimum_help()}
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
				label={i18n.quality_allow_upgrades()}
				description={i18n.quality_upgrades_help()}
			/>
		{/snippet}
	</form.Field>
</div>
