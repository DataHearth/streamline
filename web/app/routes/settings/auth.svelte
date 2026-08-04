<script lang="ts">
	import {
		createQuery,
		createMutation,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { createForm } from "@tanstack/svelte-form";
	import { KeyRound, RefreshCw, Check, Clipboard } from "@lucide/svelte";
	import { api } from "../../lib/api";
	import { config, READONLY_HINT } from "../../lib/config.svelte";
	import { toast } from "../../lib/toast";
	import { authConfigPatch } from "../../lib/schemas";
	import type { AuthConfig, UserRole } from "../../lib/types";
	import TextField from "../../components/forms/TextField.svelte";
	import Select from "../../components/forms/Select.svelte";
	import RadioCards from "../../components/forms/RadioCards.svelte";
	import SubmitButton from "../../components/forms/SubmitButton.svelte";
	import ReadOnlyFieldset from "../../components/settings/ReadOnlyFieldset.svelte";
	import Dialog from "../../components/modals/Dialog.svelte";
	import Modal from "../../components/modals/Modal.svelte";

	const qc = useQueryClient();

	let confirmRotate = $state(false);

	const cfg = createQuery<AuthConfig>(() => ({
		queryKey: ["config", "auth"],
		queryFn: () => api<AuthConfig>("/config/auth"),
	}));

	const save = createMutation<AuthConfig, Error, AuthConfig>(() => ({
		mutationFn: (body) =>
			api<AuthConfig>("/config/auth", { method: "PATCH", body }),
		onSuccess: (resp) => {
			qc.setQueryData(["config", "auth"], resp);
			// Re-seed from the response so the form goes clean again; otherwise it
			// stays dirty and seedFrom would never accept a later refetch.
			seedFrom(resp);
			toast.ok("Auth settings saved");
		},
		onError: (err) => toast.err(err.message),
	}));

	// Read-only instances can't persist the new secret, so the API hands it
	// over first (pending) and only applies it on a second, confirmed call —
	// the operator has to get it into the config file they manage.
	let pendingSecret = $state<string | null>(null);

	type RotateResult = { token?: string; pending?: boolean; secret?: string };

	const rotate = createMutation<RotateResult, Error, boolean>(() => ({
		mutationFn: (confirmed) =>
			api<RotateResult>("/auth/jwt/rotate", {
				method: "POST",
				body: { confirmed },
			}),
		onSuccess: (resp) => {
			if (resp.pending) {
				pendingSecret = resp.secret ?? "";
				return;
			}
			pendingSecret = null;
			toast.ok("JWT secret rotated — other sessions invalidated");
		},
		onError: (err) => toast.err(err.message),
	}));

	async function copySecret() {
		try {
			await navigator.clipboard.writeText(pendingSecret ?? "");
			toast.ok("Copied");
		} catch {
			toast.err("Clipboard unavailable");
		}
	}

	const form = createForm(() => ({
		defaultValues: {
			registration_mode: (cfg.data?.registration_mode ??
				"open") as AuthConfig["registration_mode"],
			session_ttl: cfg.data?.session_ttl ?? "168h",
			oidc_default_role: (cfg.data?.oidc_default_role ??
				"member") as UserRole,
		},
		validators: { onChange: authConfigPatch },
		onSubmit: ({ value }) => save.mutate(value),
	}));

	function seedFrom(data: AuthConfig) {
		form.reset({
			registration_mode: data.registration_mode,
			session_ttl: data.session_ttl,
			oidc_default_role: data.oidc_default_role,
		});
	}

	// The query refetches on window focus, so this effect fires again every time
	// the tab regains focus — re-seed only while the form is clean, or a refetch
	// silently throws away whatever the user was typing. Reading form.state adds
	// no dependency here (it is a TanStack store, not a rune), so the effect
	// still keys off cfg.data alone.
	$effect(() => {
		if (!cfg.data || form.state.isDirty) return;
		seedFrom(cfg.data);
	});

	const modes = [
		{
			value: "open",
			label: "Open",
			sub: "Anyone with the URL can register.",
		},
		{
			value: "invite",
			label: "Invite-only",
			sub: "Only invited emails can register.",
		},
		{
			value: "disabled",
			label: "Closed",
			sub: "Registration is turned off.",
		},
	] as const;
</script>

<div class="mx-auto max-w-4xl">
	<header>
		<h1 class="text-2xl font-bold tracking-tight text-fg">Authentication</h1>
		<p class="mt-1 text-sm text-fg-muted">
			Who is allowed to register, and how long sessions stay valid. Changes
			take effect immediately — no restart required.
		</p>
	</header>

	{#if cfg.isPending}
		<p class="mt-6 text-sm text-fg-subtle">Loading…</p>
	{:else if cfg.isError}
		<p class="mt-6 text-sm text-status-failed">
			Failed to load auth settings: {cfg.error?.message}
		</p>
	{:else}
		<form
			class="mt-6"
			onsubmit={(e) => {
				e.preventDefault();
				form.handleSubmit();
			}}
		>
			<ReadOnlyFieldset class="space-y-6">
			<form.Field name="registration_mode">
				{#snippet children(field)}
					<RadioCards
						legend="Registration mode"
						columns={3}
						name={field.name}
						value={field.state.value}
						onChange={(v) => field.handleChange(v)}
						options={modes.map((m) => ({
							value: m.value,
							label: m.label,
							description: m.sub,
						}))}
					/>
				{/snippet}
			</form.Field>

			<div class="grid gap-4 sm:grid-cols-2">
				<form.Field name="session_ttl">
					{#snippet children(field)}
						<TextField
							{field}
							label="Session TTL"
							placeholder="168h"
							help="Go duration (e.g. 30m, 12h, 168h). Applies to tokens issued after save."
						/>
					{/snippet}
				</form.Field>

				<form.Field name="oidc_default_role">
					{#snippet children(field)}
						<div>
							<Select
								label="OIDC default role"
								value={field.state.value as UserRole}
								options={[
									{ value: "admin", label: "Admin" },
									{ value: "member", label: "Member" },
									{ value: "request_only", label: "Request-only" },
								]}
								onChange={(role) => field.handleChange(role)}
							/>
							<p class="mt-1 text-xs text-fg-muted">
								Assigned to new users created via OIDC when registration
								is open.
							</p>
						</div>
					{/snippet}
				</form.Field>
			</div>

			<div class="flex justify-end gap-2">
				<SubmitButton
				{form}
				label="Save changes"
				pendingLabel="Saving…"
				disabled={config.readOnly}
				title={config.readOnly ? READONLY_HINT : undefined}
			/>
				<button
					type="submit"
					hidden
					aria-hidden="true"
					tabindex="-1"
				></button>
				<span class="inline-flex items-center gap-1.5 text-xs text-fg-subtle">
					<Check size={12} aria-hidden="true" />
					Applied immediately
				</span>
			</div>
			</ReadOnlyFieldset>
		</form>

		<section class="mt-6 rounded-lg border border-border bg-bg-card p-4">
			<header class="flex items-start gap-2.5">
				<span
					class="grid h-8 w-8 shrink-0 place-items-center rounded-md bg-status-failed/10 text-status-failed"
				>
					<KeyRound size={16} aria-hidden="true" />
				</span>
				<div class="min-w-0 flex-1">
					<h3 class="text-sm font-semibold text-fg">JWT signing secret</h3>
					<p class="mt-0.5 text-xs text-fg-muted">
						Rotate the HMAC secret used to sign session tokens. Every
						active session is invalidated immediately — including those of
						other admins. You will stay signed in.{config.readOnly
							? " This instance is read-only, so you get the new secret to save into your config before it takes effect."
							: ""}
					</p>
				</div>
			</header>
			<div class="mt-3 flex justify-end">
				<button
					type="button"
					disabled={rotate.isPending}
					onclick={() =>
						config.readOnly
							? rotate.mutate(false)
							: (confirmRotate = true)}
					class="inline-flex h-9 items-center gap-1.5 rounded-md border border-status-failed/40 bg-status-failed/10 px-3 text-sm font-medium text-status-failed transition hover:bg-status-failed/15 disabled:cursor-not-allowed disabled:opacity-60"
				>
					<RefreshCw size={14} aria-hidden="true" />
					{rotate.isPending ? "Rotating…" : "Rotate secret"}
				</button>
			</div>
		</section>
	{/if}
</div>

<Dialog
	open={confirmRotate}
	title="Rotate the JWT secret?"
	body="This signs everyone else out. You will stay signed in."
	onClose={() => (confirmRotate = false)}
	actions={[
		{ label: "Cancel", variant: "ghost", autofocus: true },
		{
			label: "Rotate secret",
			variant: "danger",
			onClick: () => rotate.mutate(true),
		},
	]}
/>

<Modal
	open={pendingSecret !== null}
	title="Save the new signing secret"
	size="lg"
	onClose={() => (pendingSecret = null)}
>
	<p class="text-sm text-fg-muted">
		Nothing has changed yet. Copy this into <code
			class="rounded bg-bg-deep px-1 py-0.5 font-mono text-xs text-fg"
			>auth.session_secret</code
		> in the config this instance reads, then apply — the rotation signs everyone
		else out and won't survive a restart unless the file carries it.
	</p>
	<code
		class="mt-3 block break-all rounded-md bg-bg-deep p-3 font-mono text-xs text-fg"
	>
		{pendingSecret}
	</code>
	<button
		type="button"
		onclick={copySecret}
		class="mt-2 inline-flex h-8 items-center gap-1.5 rounded-md border border-border px-2.5 text-xs font-medium text-fg-muted transition hover:bg-surface hover:text-fg"
	>
		<Clipboard size={13} aria-hidden="true" />
		Copy secret
	</button>

	{#snippet footer()}
		<button
			type="button"
			data-autofocus
			onclick={() => (pendingSecret = null)}
			class="inline-flex h-9 items-center rounded-md border border-border px-3 text-sm text-fg-muted transition hover:text-fg"
		>
			Cancel
		</button>
		<button
			type="button"
			disabled={rotate.isPending}
			onclick={() => rotate.mutate(true)}
			class="inline-flex h-9 items-center gap-1.5 rounded-md border border-status-failed/40 bg-status-failed/10 px-3 text-sm font-medium text-status-failed transition hover:bg-status-failed/15 disabled:cursor-not-allowed disabled:opacity-60"
		>
			<RefreshCw size={14} aria-hidden="true" />
			{rotate.isPending ? "Applying…" : "I saved it — apply now"}
		</button>
	{/snippet}
</Modal>
