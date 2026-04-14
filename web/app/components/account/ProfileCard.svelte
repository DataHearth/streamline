<script lang="ts">
	import { createForm } from "@tanstack/svelte-form";
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import * as v from "valibot";
	import { api } from "../../lib/api";
	import { auth } from "../../lib/auth.svelte";
	import { toast } from "../../lib/toast";
	import { displayName } from "../../lib/schemas";
	import type { User } from "../../lib/types";
	import TextField from "../forms/TextField.svelte";
	import Modal from "../modals/Modal.svelte";

	const qc = useQueryClient();

	let open = $state(false);

	const mutation = createMutation<User, Error, { display_name: string }>(() => ({
		mutationFn: (body) => api<User>("/auth/me", { method: "PATCH", body }),
		onSuccess: (user) => {
			auth.user = user;
			qc.invalidateQueries({ queryKey: ["auth", "me"] });
			toast.ok("Profile updated");
			open = false;
		},
		onError: (err) => toast.err(err.message),
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
		<h3 class="text-base font-semibold text-fg">Profile</h3>
		<button
			type="button"
			onclick={startEdit}
			class="inline-flex h-7 items-center rounded-md border border-border bg-surface px-3 text-xs font-medium text-fg-muted transition hover:bg-surface-2 hover:text-fg"
		>
			Edit
		</button>
	</header>

	<dl class="grid gap-2.5 text-sm">
		<div class="grid grid-cols-[110px_1fr] items-baseline gap-3">
			<dt class="text-fg-subtle">Display name</dt>
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
			<dt class="text-fg-subtle">Email</dt>
			<dd
				class="truncate font-mono text-fg"
				title={auth.user?.email ?? ""}
			>
				{auth.user?.email ?? "—"}
			</dd>
		</div>
		<div class="grid grid-cols-[110px_1fr] items-baseline gap-3">
			<dt class="text-fg-subtle">Role</dt>
			<dd class="text-fg capitalize">{auth.user?.role ?? "member"}</dd>
		</div>
		<div class="grid grid-cols-[110px_1fr] items-baseline gap-3">
			<dt class="text-fg-subtle">Auth method</dt>
			<dd class="text-fg">
				{auth.user?.auth_method === "both"
					? "Password + SSO"
					: auth.user?.auth_method === "oidc"
						? "SSO"
						: "Password"}
			</dd>
		</div>
	</dl>
</section>

<Modal
	{open}
	title="Edit profile"
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
			<span class="mb-1 block text-sm font-medium text-fg">Email</span>
			<div
				class="w-full rounded-md border border-border bg-bg px-3 py-2 font-mono text-sm text-fg-muted opacity-70"
			>
				{auth.user?.email ?? "—"}
			</div>
			<span class="mt-1 block text-xs text-fg-subtle">
				Email changes are not supported yet.
			</span>
		</div>
		<form.Field name="display_name">
			{#snippet children(field)}
				<TextField
					{field}
					label="Display name"
					autocomplete="name"
					placeholder="Leave blank to use email"
				/>
			{/snippet}
		</form.Field>
	</form>

	{#snippet footer()}
		<button
			type="button"
			onclick={() => {
				if (!mutation.isPending) open = false;
			}}
			disabled={mutation.isPending}
			class="inline-flex h-9 items-center rounded-md border border-border bg-surface px-3.5 text-sm font-medium text-fg-muted transition hover:bg-surface-2 hover:text-fg disabled:cursor-not-allowed disabled:opacity-60"
		>
			Cancel
		</button>
		<button
			type="submit"
			form="edit-profile-form"
			disabled={mutation.isPending}
			class="inline-flex h-9 items-center rounded-md bg-accent px-3.5 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
		>
			{mutation.isPending ? "Saving…" : "Save changes"}
		</button>
	{/snippet}
</Modal>
