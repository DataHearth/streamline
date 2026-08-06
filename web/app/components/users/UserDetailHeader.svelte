<script lang="ts">
	import { createForm } from "@tanstack/svelte-form";
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import * as v from "valibot";
	import { api, errorText } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { displayName, email as emailSchema } from "../../lib/schemas";
	import type { AuthMethod, User, UserRole } from "../../lib/types";
	import TextField from "../forms/TextField.svelte";
	import SubmitButton from "../forms/SubmitButton.svelte";
	import Select from "../forms/Select.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	const ROLE_OPTIONS: { value: UserRole; label: string }[] = [
		{ value: "member", label: i18n.role_member() },
		{ value: "request_only", label: i18n.role_request_only() },
		{ value: "admin", label: i18n.common_admin() },
	];
	const AUTH_OPTIONS: { value: AuthMethod; label: string }[] = [
		{ value: "local", label: i18n.auth_method_local() },
		{ value: "oidc", label: "OIDC" },
		{ value: "both", label: i18n.auth_method_both() },
	];

	let { user }: { user: User } = $props();

	const qc = useQueryClient();

	type Patch = {
		email?: string;
		display_name?: string;
		role?: UserRole;
		auth_method?: AuthMethod;
	};

	const patch = createMutation<User, Error, Patch>(() => ({
		mutationFn: (body) =>
			api<User>(`/users/${user.id}`, { method: "PATCH", body }),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["user", user.id] });
			qc.invalidateQueries({ queryKey: ["users"] });
			toast.ok("Saved");
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	const form = createForm(() => ({
		defaultValues: {
			email: user.email,
			display_name: user.display_name ?? "",
			role: user.role,
			auth_method: user.auth_method,
		},
		validators: {
			onChange: v.object({
				email: emailSchema,
				display_name: displayName,
				role: v.picklist(["admin", "member", "request_only"] as const),
				auth_method: v.picklist(["local", "oidc", "both"] as const),
			}),
		},
		onSubmit: ({ value }) => patch.mutate(value),
	}));
</script>

<section class="rounded-lg border border-border bg-bg-elevated p-5">
	<header class="mb-4">
		<h3 class="text-base font-semibold text-fg">{i18n.common_profile()}</h3>
		<p class="mt-0.5 text-xs text-fg-muted">
			{i18n.users_role_immediate()}
		</p>
	</header>

	<form
		class="grid grid-cols-1 gap-3 md:grid-cols-2"
		onsubmit={(e) => {
			e.preventDefault();
			form.handleSubmit();
		}}
	>
		<form.Field name="email">
			{#snippet children(field)}
				<TextField
					{field}
					label={i18n.common_email()}
					type="email"
					autocomplete="email"
					help={i18n.users_email_unique()}
				/>
			{/snippet}
		</form.Field>

		<form.Field name="display_name">
			{#snippet children(field)}
				<TextField
					{field}
					label={i18n.common_display_name()}
					placeholder={i18n.account_blank_uses_email()}
				/>
			{/snippet}
		</form.Field>

		<form.Field name="role">
			{#snippet children(field)}
				<Select
					label={i18n.common_role()}
					value={field.state.value}
					options={ROLE_OPTIONS}
					onChange={(v) => field.handleChange(v)}
				/>
			{/snippet}
		</form.Field>

		<form.Field name="auth_method">
			{#snippet children(field)}
				<Select
					label={i18n.dlclient_auth_method()}
					value={field.state.value}
					options={AUTH_OPTIONS}
					onChange={(v) => field.handleChange(v)}
				/>
			{/snippet}
		</form.Field>

		<div class="md:col-span-2">
			<SubmitButton {form} />
		</div>
	</form>
</section>
