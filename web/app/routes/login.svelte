<script lang="ts">
	import { onMount, tick } from "svelte";
	import { slide } from "svelte/transition";
	import { ChevronDown, Mail } from "@lucide/svelte";
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
	} from "@lib/auth_api";
	import { ApiError, errorText } from "@lib/api";
	import { m as i18n } from "@lib/paraglide/messages.js";
	import { email as emailSchema } from "@lib/schemas";
	import TextField from "@components/forms/TextField.svelte";
	import AuthCard from "@components/auth/AuthCard.svelte";
	import BrandLogo from "@components/settings/BrandLogo.svelte";

	let nextParam = $state("/");
	let oidcError = $state("");

	// Resolving against our own origin defers to the browser's URL parser, which
	// is the thing that decides where assign() actually lands. Prefix checks miss
	// what it folds: "\" becomes "/" and tab/newline are stripped, so both
	// "/\evil.example" and "/<TAB>/evil.example" read as rooted paths and leave
	// the site. URLSearchParams has already percent-decoded, so "%2F%5C" arrives
	// here as "/\" too. Comparing the resolved origin catches all of them without
	// enumerating any. /auth/* is rejected so a stale next=/auth/oidc/<name>/start
	// can't bounce a local login straight back into the SSO flow.
	const isSafeNext = (n: string) => {
		try {
			const u = new URL(n, window.location.origin);
			return (
				u.origin === window.location.origin && !u.pathname.startsWith("/auth/")
			);
		} catch {
			return false;
		}
	};

	// The browser remembers that its last sign-in went through the password
	// form, so a local/break-glass account isn't one extra click every time.
	// Per-viewer convenience only: storage can be blocked, so every access is
	// guarded and the page works the same without it.
	const LOCAL_LOGIN_KEY = "streamline.login.local";
	const rememberLocal = () => {
		try {
			localStorage.setItem(LOCAL_LOGIN_KEY, "1");
		} catch {}
	};
	const rememberedLocal = () => {
		try {
			return localStorage.getItem(LOCAL_LOGIN_KEY) === "1";
		} catch {
			return false;
		}
	};

	let formOpen = $state(false);
	let reduceMotion = $state(false);
	let formEl: HTMLFormElement | undefined = $state();

	const toggleForm = async () => {
		formOpen = !formOpen;
		if (formOpen) {
			await tick();
			formEl?.querySelector<HTMLInputElement>("input")?.focus();
		}
	};

	onMount(() => {
		reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
		formOpen = rememberedLocal();
		const q = new URLSearchParams(window.location.search);
		const next = q.get("next");
		if (next && isSafeNext(next)) {
			nextParam = next;
		}
		const err = q.get("error");
		oidcError = oidcErrorMessage(err);
		// That error tells the user to sign in with their password instead.
		if (err === "oidc_link_not_allowed") formOpen = true;
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
			rememberLocal();
			window.location.assign(nextParam);
		},
		onError: (err) => {
			errorMsg = errorText(err, i18n.auth_login_failed());
			formOpen = true;
		},
	}));

	const form = createForm(() => ({
		defaultValues: { email: "", password: "" },
		validators: {
			onChange: v.object({
				email: emailSchema,
				password: v.pipe(v.string(), v.minLength(1, i18n.validation_required())),
			}),
		},
		onSubmit: ({ value }) => {
			errorMsg = "";
			login.mutate(value);
		},
	}));

	let providers = $derived(cfg.data?.providers ?? []);
	// With SSO configured the provider buttons lead and the password form folds
	// behind a disclosure; without it the form is the only way in.
	let hasSSO = $derived(providers.length > 0);
	let title = i18n.auth_login_title();
	let subtitle = i18n.auth_login_subtitle();
</script>

<svelte:head><title>{i18n.auth_login_page_title()}</title></svelte:head>

{#snippet loginForm()}
	<form
		bind:this={formEl}
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
					label={i18n.field_email()}
					type="email"
					autocomplete="username"
				/>
			{/snippet}
		</form.Field>
		<form.Field name="password">
			{#snippet children(field)}
				<TextField
					{field}
					label={i18n.field_password()}
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
			{login.isPending ? i18n.auth_signing_in() : i18n.auth_sign_in()}
		</button>
	</form>
{/snippet}

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

	{#if hasSSO}
		<div class="grid gap-2">
			{#each providers as p, i}
				<a
					href={oidcStartURL(p.name, nextParam)}
					target="_self"
					class={[
						"inline-flex h-11 w-full items-center justify-center gap-3 rounded-md px-4 text-sm transition-colors focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2",
						i === 0
							? "bg-accent font-semibold text-fg-on-accent hover:bg-accent-hover"
							: "border border-border-strong bg-surface font-medium text-fg hover:border-accent hover:bg-surface-2",
					]}
				>
					<BrandLogo name={p.name} size={20} />
					{i18n.auth_continue_with_provider({ provider: p.name })}
				</a>
			{/each}
		</div>

		<div class="relative my-5">
			<div class="absolute inset-0 flex items-center" aria-hidden="true">
				<div class="w-full border-t border-border"></div>
			</div>
			<div class="relative flex justify-center text-xs uppercase">
				<span
					class="bg-bg-elevated px-2 font-mono tracking-wider text-fg-faint"
					>{i18n.auth_or()}</span
				>
			</div>
		</div>

		<button
			type="button"
			aria-expanded={formOpen}
			aria-controls="local-login"
			onclick={toggleForm}
			class="inline-flex h-11 w-full items-center justify-center gap-2 rounded-md text-sm font-medium text-fg-muted transition-colors hover:text-fg focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2"
		>
			<Mail size={16} aria-hidden="true" />
			{i18n.auth_sign_in_with_email()}
			<ChevronDown
				size={16}
				aria-hidden="true"
				class={["transition-transform", formOpen && "rotate-180"]}
			/>
		</button>
		{#if formOpen}
			<div
				id="local-login"
				class="pt-4"
				transition:slide={{ duration: reduceMotion ? 0 : 180 }}
			>
				{@render loginForm()}
			</div>
		{/if}
	{:else}
		{@render loginForm()}
	{/if}

	{#snippet footer()}
		{i18n.auth_no_account()}
		<a
			href="/register"
			class="touch-hit inline-block cursor-pointer rounded font-medium text-accent hover:text-accent-hover hover:underline focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-2"
		>
			{i18n.auth_create_one()}
		</a>
	{/snippet}
</AuthCard>
