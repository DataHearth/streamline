<script lang="ts">
	import { onMount } from "svelte";
	import { createMutation, createQuery } from "@tanstack/svelte-query";
	import { createForm } from "@tanstack/svelte-form";
	import * as v from "valibot";
	import {
		fetchAuthConfig,
		fetchInvitePrefill,
		postRegister,
		type AuthConfig,
		type RegisterInput,
	} from "../lib/auth_api";
	import { ApiError } from "../lib/api";
	import { email as emailSchema, password as passwordSchema } from "../lib/schemas";
	import TextField from "../components/forms/TextField.svelte";
	import AuthCard from "../components/auth/AuthCard.svelte";

	let token = $state("");
	let emailLocked = $state(false);
	let prefillError = $state("");

	onMount(async () => {
		const q = new URLSearchParams(window.location.search);
		const t = q.get("token") ?? "";
		token = t;
		if (t) {
			try {
				const inv = await fetchInvitePrefill(t);
				if (inv.email) {
					emailLocked = inv.email_locked;
					form.setFieldValue("email", inv.email);
				}
			} catch (err) {
				prefillError = err instanceof ApiError ? err.message : "Invite invalid";
			}
		}
	});

	const cfg = createQuery<AuthConfig>(() => ({
		queryKey: ["auth", "config"],
		queryFn: fetchAuthConfig,
		staleTime: Infinity,
	}));

	let errorMsg = $state("");

	const register = createMutation<null, Error, RegisterInput>(() => ({
		mutationFn: postRegister,
		onSuccess: () => {
			window.location.assign("/dashboard");
		},
		onError: (err) => {
			errorMsg = err instanceof ApiError ? err.message : "Registration failed";
		},
	}));

	const form = createForm(() => ({
		defaultValues: {
			email: "",
			password: "",
			confirm: "",
			display_name: "",
		},
		validators: {
			onChange: v.pipe(
				v.object({
					email: emailSchema,
					password: passwordSchema,
					confirm: v.string(),
					display_name: v.string(),
				}),
				v.forward(
					v.check(
						(input) => input.password === input.confirm,
						"Passwords do not match",
					),
					["confirm"],
				),
			),
		},
		onSubmit: ({ value }) => {
			errorMsg = "";
			register.mutate({
				email: value.email,
				password: value.password,
				confirm: value.confirm,
				display_name: value.display_name || undefined,
				token: token || undefined,
			});
		},
	}));

	let mode = $derived(cfg.data?.registration_mode ?? "open");
	let blocked = $derived(mode === "disabled");
	let needsInvite = $derived(mode === "invite" && !token);
	let title = $derived(token ? "Accept invite" : "Create your account");
	let subtitle = "Welcome to Streamline.";
</script>

<svelte:head><title>Create account — Streamline</title></svelte:head>

<AuthCard {title} {subtitle} eyebrow="Streamline">
	{#if blocked}
		<p
			role="alert"
			class="rounded-md border border-status-failed/40 bg-status-failed/10 px-3 py-2 text-sm text-status-failed"
		>
			Registration is disabled. Ask an admin for an invite or to enable open
			registration.
		</p>
	{:else if needsInvite}
		<p
			role="alert"
			class="rounded-md border border-status-wanted/40 bg-status-wanted/10 px-3 py-2 text-sm text-status-wanted"
		>
			An invite token is required. Use the link an admin sent you.
		</p>
	{:else}
		{#if prefillError}
			<p
				role="alert"
				class="mb-4 rounded-md border border-status-failed/40 bg-status-failed/10 px-3 py-2 text-sm text-status-failed"
			>
				{prefillError}
			</p>
		{/if}
		{#if errorMsg}
			<p
				role="alert"
				class="mb-4 rounded-md border border-status-failed/40 bg-status-failed/10 px-3 py-2 text-sm text-status-failed"
			>
				{errorMsg}
			</p>
		{/if}

		<form
			class="space-y-4"
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
						autocomplete="username"
						readonly={emailLocked}
					/>
				{/snippet}
			</form.Field>
			<form.Field name="display_name">
				{#snippet children(field)}
					<TextField
						{field}
						label="Display name (optional)"
						autocomplete="nickname"
					/>
				{/snippet}
			</form.Field>
			<form.Field name="password">
				{#snippet children(field)}
					<TextField
						{field}
						label="Password"
						type="password"
						autocomplete="new-password"
						help="Minimum 8 characters."
					/>
				{/snippet}
			</form.Field>
			<form.Field name="confirm">
				{#snippet children(field)}
					<TextField
						{field}
						label="Confirm password"
						type="password"
						autocomplete="new-password"
					/>
				{/snippet}
			</form.Field>
			<button
				type="submit"
				disabled={!form.state.canSubmit || register.isPending}
				class="inline-flex h-11 w-full items-center justify-center gap-2 rounded-md bg-accent text-sm font-semibold text-fg-on-accent transition-colors hover:bg-accent-hover focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2 disabled:cursor-not-allowed disabled:opacity-60"
			>
				{register.isPending ? "Creating account…" : "Create account"}
			</button>
		</form>
	{/if}

	{#snippet footer()}
		Already have an account?
		<a
			href="/login"
			class="cursor-pointer rounded font-medium text-accent hover:text-accent-hover hover:underline focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2"
		>
			Sign in
		</a>
	{/snippet}
</AuthCard>
