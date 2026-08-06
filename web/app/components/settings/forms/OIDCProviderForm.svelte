<script lang="ts">
	import type { FormApi } from "@tanstack/svelte-form";
	import TextField from "../../forms/TextField.svelte";
	import { m as i18n } from "../../../lib/paraglide/messages.js";

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
				label={i18n.common_name()}
				placeholder="authentik"
				help={i18n.oidc_name_help()}
			/>
		{/snippet}
	</form.Field>

	<form.Field name="issuer">
		{#snippet children(field)}
			<TextField
				{field}
				label={i18n.oidc_issuer()}
				placeholder="https://auth.example.com/application/o/streamline/"
				help={i18n.oidc_issuer_help()}
			/>
		{/snippet}
	</form.Field>

	<div class="grid gap-3 sm:grid-cols-2">
		<form.Field name="client_id">
			{#snippet children(field)}
				<TextField {field} label={i18n.oidc_client_id()} autocomplete="off" />
			{/snippet}
		</form.Field>
		<form.Field name="client_secret">
			{#snippet children(field)}
				<TextField
					{field}
					label={i18n.oidc_client_secret()}
					type="password"
					autocomplete="off"
				/>
			{/snippet}
		</form.Field>
	</div>
</div>
