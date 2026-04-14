<script lang="ts">
	import type { FormApi } from "@tanstack/svelte-form";
	import TextField from "../../forms/TextField.svelte";

	type Values = {
		name: string;
		issuer: string;
		client_id: string;
		client_secret: string;
	};

	type Props = { form: FormApi<Values, undefined> };
	let { form }: Props = $props();
</script>

<div class="space-y-4">
	<form.Field name="name">
		{#snippet children(field)}
			<TextField
				{field}
				label="Name"
				placeholder="authentik"
				help="Internal identifier. Letters, digits, dash, underscore."
			/>
		{/snippet}
	</form.Field>

	<form.Field name="issuer">
		{#snippet children(field)}
			<TextField
				{field}
				label="Issuer URL"
				placeholder="https://auth.example.com/application/o/streamline/"
				help="Discovery document is fetched from this URL on save."
			/>
		{/snippet}
	</form.Field>

	<div class="grid gap-3 sm:grid-cols-2">
		<form.Field name="client_id">
			{#snippet children(field)}
				<TextField {field} label="Client ID" autocomplete="off" />
			{/snippet}
		</form.Field>
		<form.Field name="client_secret">
			{#snippet children(field)}
				<TextField
					{field}
					label="Client secret"
					type="password"
					autocomplete="off"
				/>
			{/snippet}
		</form.Field>
	</div>
</div>
