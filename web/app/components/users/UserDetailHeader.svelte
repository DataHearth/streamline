<script lang="ts">
	import { createForm } from "@tanstack/svelte-form";
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import * as v from "valibot";
	import { api } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { displayName, email as emailSchema } from "../../lib/schemas";
	import type { AuthMethod, User, UserRole } from "../../lib/types";
	import TextField from "../forms/TextField.svelte";
	import SubmitButton from "../forms/SubmitButton.svelte";
	import Select from "../forms/Select.svelte";

	const ROLE_OPTIONS: { value: UserRole; label: string }[] = [
		{ value: "member", label: "Member" },
		{ value: "request_only", label: "Request only" },
		{ value: "admin", label: "Admin" },
	];
	const AUTH_OPTIONS: { value: AuthMethod; label: string }[] = [
		{ value: "local", label: "Local" },
		{ value: "oidc", label: "OIDC" },
		{ value: "both", label: "Both" },
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
		onError: (err) => toast.err(err.message),
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
		<h3 class="text-base font-semibold text-fg">Profile</h3>
		<p class="mt-0.5 text-xs text-fg-muted">
			Role and auth method take effect immediately.
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
					label="Email"
					type="email"
					autocomplete="email"
					help="Must be unique across users."
				/>
			{/snippet}
		</form.Field>

		<form.Field name="display_name">
			{#snippet children(field)}
				<TextField
					{field}
					label="Display name"
					placeholder="Leave blank to use email"
				/>
			{/snippet}
		</form.Field>

		<form.Field name="role">
			{#snippet children(field)}
				<Select
					label="Role"
					value={field.state.value}
					options={ROLE_OPTIONS}
					onChange={(v) => field.handleChange(v)}
				/>
			{/snippet}
		</form.Field>

		<form.Field name="auth_method">
			{#snippet children(field)}
				<Select
					label="Auth method"
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
