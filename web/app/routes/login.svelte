<script lang="ts">
	import { onMount } from "svelte";
	import { createMutation, createQuery } from "@tanstack/svelte-query";
	import { createForm } from "@tanstack/svelte-form";
	import * as v from "valibot";
	import {
		fetchAuthConfig,
		oidcErrorMessage,
		oidcStartURL,
		postLogin,
		type AuthConfig,
		type LoginInput,
	} from "../lib/auth_api";
	import { ApiError } from "../lib/api";
	import { email as emailSchema } from "../lib/schemas";
	import TextField from "../components/forms/TextField.svelte";
	import AuthCard from "../components/auth/AuthCard.svelte";
	import BrandLogo from "../components/settings/BrandLogo.svelte";

	let nextParam = $state("/dashboard");
	let oidcError = $state("");

	onMount(() => {
		const q = new URLSearchParams(window.location.search);
		const next = q.get("next");
		// Reject /auth/* so a stale next=/auth/oidc/<name>/start can't bounce a
		// local login straight back into the SSO flow.
		if (
			next &&
			next.startsWith("/") &&
			!next.startsWith("//") &&
			!next.startsWith("/auth/")
		) {
			nextParam = next;
		}
		oidcError = oidcErrorMessage(q.get("error"));
	});

	const cfg = createQuery<AuthConfig>(() => ({
		queryKey: ["auth", "config"],
		queryFn: fetchAuthConfig,
		staleTime: Infinity,
	}));

	let errorMsg = $state("");

	const login = createMutation<null, Error, LoginInput>(() => ({
		mutationFn: postLogin,
		onSuccess: () => {
			window.location.assign(nextParam);
		},
		onError: (err) => {
			errorMsg = err instanceof ApiError ? err.message : "Sign-in failed";
		},
	}));

	const form = createForm(() => ({
		defaultValues: { email: "", password: "" },
		validators: {
			onChange: v.object({
				email: emailSchema,
				password: v.pipe(v.string(), v.minLength(1, "Required")),
			}),
		},
		onSubmit: ({ value }) => {
			errorMsg = "";
			login.mutate(value);
		},
	}));

	let providers = $derived(cfg.data?.providers ?? []);
	let title = "Welcome back";
	let subtitle = "Your media, unified.";
</script>

<svelte:head><title>Sign in — Streamline</title></svelte:head>

<AuthCard {title} {subtitle} eyebrow="Streamline">
	{#if oidcError}
		<p
			role="alert"
			class="mb-4 rounded-md border border-status-failed/40 bg-status-failed/10 px-3 py-2 text-sm text-status-failed"
		>
			{oidcError}
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
				/>
			{/snippet}
		</form.Field>
		<form.Field name="password">
			{#snippet children(field)}
				<TextField
					{field}
					label="Password"
					type="password"
					autocomplete="current-password"
				/>
			{/snippet}
		</form.Field>
		<button
			type="submit"
			disabled={!form.state.canSubmit || login.isPending}
			class="inline-flex h-11 w-full items-center justify-center gap-2 rounded-md bg-accent text-sm font-semibold text-fg-on-accent transition-colors hover:bg-accent-hover focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2 disabled:cursor-not-allowed disabled:opacity-60"
		>
			{login.isPending ? "Signing in…" : "Sign in"}
		</button>
	</form>

	{#if providers.length > 0}
		<div class="relative my-5">
			<div class="absolute inset-0 flex items-center" aria-hidden="true">
				<div class="w-full border-t border-border"></div>
			</div>
			<div class="relative flex justify-center text-xs uppercase">
				<span
					class="bg-bg-elevated px-2 font-mono tracking-wider text-fg-faint"
					>or continue with</span
				>
			</div>
		</div>
		<div class="grid gap-2">
			{#each providers as p}
				<a
					href={oidcStartURL(p.name, nextParam)}
					target="_self"
					class="inline-flex h-11 w-full items-center justify-center gap-3 rounded-md border border-border-strong bg-surface px-4 text-sm font-medium text-fg transition-colors hover:border-accent hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2"
				>
					<BrandLogo name={p.name} size={20} />
					Continue with {p.name}
				</a>
			{/each}
		</div>
	{/if}

	{#snippet footer()}
		No account?
		<a
			href="/register"
			class="cursor-pointer rounded font-medium text-accent hover:text-accent-hover hover:underline focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2"
		>
			Create one
		</a>
	{/snippet}
</AuthCard>
