<script lang="ts">
	import Button from "@components/shared/Button.svelte";
	import { createForm } from "@tanstack/svelte-form";
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import * as v from "valibot";
	import { api, errorText } from "@lib/api";
	import { auth } from "@lib/auth.svelte";
	import { toast } from "@lib/toast";
	import { displayName } from "@lib/schemas";
	import type { User } from "@lib/types";
	import TextField from "@components/forms/TextField.svelte";
	import Modal from "@components/modals/Modal.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	const qc = useQueryClient();

	let open = $state(false);

	const mutation = createMutation<User, Error, { display_name: string }>(() => ({
		mutationFn: (body) => api<User>("/auth/me", { method: "PATCH", body }),
		onSuccess: (user) => {
			auth.user = user;
			qc.invalidateQueries({ queryKey: ["auth", "me"] });
			toast.ok(i18n.account_profile_updated());
			open = false;
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	const form = createForm(() => ({
		defaultValues: { display_name: auth.user?.display_name ?? "" },
		validators: { onChange: v.object({ display_name: displayName }) },
		onSubmit: ({ value }) => mutation.mutate(value),
	}));

	function startEdit() {
		form.reset({ display_name: auth.user?.display_name ?? "" });
		open = true;
	}
</script>

<section class="rounded-lg border border-border bg-bg-elevated p-6">
	<header class="mb-4 flex items-start justify-between gap-3">
		<h3 class="text-base font-semibold text-fg">{i18n.common_profile()}</h3>
		<Button
			variant="secondary"
			size="sm"
			type="button"
			onclick={startEdit}
		>
			{i18n.common_edit()}
		</Button>
	</header>

	<dl class="grid gap-2.5 text-sm">
		<div class="grid grid-cols-[110px_1fr] items-baseline gap-3">
			<dt class="text-fg-subtle">{i18n.common_display_name()}</dt>
			<dd
				class="truncate text-fg"
				title={auth.user?.display_name?.trim() ?? ""}
			>
				{#if auth.user?.display_name?.trim()}
					{auth.user.display_name}
				{:else}
					<span class="text-fg-faint">—</span>
				{/if}
			</dd>
		</div>
		<div class="grid grid-cols-[110px_1fr] items-baseline gap-3">
			<dt class="text-fg-subtle">{i18n.common_email()}</dt>
			<dd
				class="truncate font-mono text-fg"
				title={auth.user?.email ?? ""}
			>
				{auth.user?.email ?? "—"}
			</dd>
		</div>
		<div class="grid grid-cols-[110px_1fr] items-baseline gap-3">
			<dt class="text-fg-subtle">{i18n.common_role()}</dt>
			<dd class="text-fg capitalize">{auth.user?.role ?? "member"}</dd>
		</div>
		<div class="grid grid-cols-[110px_1fr] items-baseline gap-3">
			<dt class="text-fg-subtle">{i18n.dlclient_auth_method()}</dt>
			<dd class="text-fg">
				{auth.user?.auth_method === "both"
					? i18n.account_password_sso()
					: auth.user?.auth_method === "oidc"
						? i18n.common_sso()
						: i18n.common_password()}
			</dd>
		</div>
	</dl>
</section>

<Modal
	{open}
	title={i18n.account_edit_profile()}
	size="md"
	onClose={() => {
		if (!mutation.isPending) open = false;
	}}
>
	<form
		id="edit-profile-form"
		class="grid gap-3"
		onsubmit={(e) => {
			e.preventDefault();
			form.handleSubmit();
		}}
	>
		<div class="block">
			<span class="mb-1 block text-sm font-medium text-fg">{i18n.common_email()}</span>
			<div
				class="w-full rounded-md border border-border bg-bg px-3 py-2 font-mono text-sm text-fg-muted opacity-70"
			>
				{auth.user?.email ?? "—"}
			</div>
			<span class="mt-1 block text-xs text-fg-subtle">
				{i18n.account_email_immutable()}
			</span>
		</div>
		<form.Field name="display_name">
			{#snippet children(field)}
				<TextField
					{field}
					label={i18n.common_display_name()}
					autocomplete="name"
					placeholder={i18n.account_blank_uses_email()}
				/>
			{/snippet}
		</form.Field>
	</form>

	{#snippet footer()}
		<Button
			variant="secondary"
			type="button"
			onclick={() => {
				if (!mutation.isPending) open = false;
			}}
			disabled={mutation.isPending}
		
		>
			{i18n.common_cancel()}
		</Button>
		<Button
			type="submit"
			form="edit-profile-form"
			disabled={mutation.isPending}
		>
			{mutation.isPending ? i18n.common_saving() : i18n.common_save_changes()}
		</Button>
	{/snippet}
</Modal>
