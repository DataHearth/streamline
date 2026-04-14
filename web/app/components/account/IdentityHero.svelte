<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import { Shield, KeyRound, Monitor, ShieldCheck } from "@lucide/svelte";
	import { api } from "../../lib/api";
	import { auth } from "../../lib/auth.svelte";
	import { formatDateTime, formatRelative } from "../../lib/dates";
	import type { ApiKey, Session } from "../../lib/types";

	const sessions = createQuery<Session[]>(() => ({
		queryKey: ["auth", "me", "sessions"],
		queryFn: () => api<Session[]>("/auth/me/sessions"),
	}));
	const keys = createQuery<ApiKey[]>(() => ({
		queryKey: ["auth", "me", "api-keys"],
		queryFn: () => api<ApiKey[]>("/auth/me/api-keys"),
	}));

	let user = $derived(auth.user);

	let initials = $derived.by(() => {
		const src = user?.display_name?.trim() || user?.email || "?";
		const parts = src.split(/[\s@._-]+/).filter(Boolean);
		const a = parts[0]?.[0] ?? "?";
		const b = parts[1]?.[0] ?? "";
		return (a + b).toUpperCase();
	});

	let primary = $derived(
		user?.display_name?.trim() || user?.email || "Unnamed",
	);

	let sessionCount = $derived(sessions.data?.length ?? 0);
	let keyCount = $derived(keys.data?.length ?? 0);

	let currentSession = $derived(sessions.data?.find((s) => s.is_current));
	let lastSeen = $derived(
		currentSession?.last_seen_at ?? currentSession?.created_at ?? null,
	);

	let authMethodLabel = $derived(
		user?.auth_method === "both"
			? "local + SSO"
			: user?.auth_method === "oidc"
				? "SSO"
				: user?.auth_method === "local"
					? "password"
					: null,
	);
</script>

<section
	class="relative overflow-hidden rounded-xl border border-border bg-bg-elevated"
>
	<div class="hero-glow pointer-events-none absolute inset-0" aria-hidden="true"></div>

	<div
		class="relative flex flex-wrap items-center justify-between gap-6 p-6 md:p-7"
	>
		<div class="flex min-w-0 flex-1 items-center gap-4">
			<div
				class="grid h-16 w-16 shrink-0 place-items-center rounded-full bg-accent-soft text-xl font-semibold text-accent-text ring-1 ring-inset ring-accent-line"
				aria-hidden="true"
			>
				{initials}
			</div>
			<div class="min-w-0 flex-1">
				<div class="flex flex-wrap items-center gap-2">
					<h2
						class="truncate text-2xl font-bold tracking-tight text-fg"
					>
						{primary}
					</h2>
					{#if user?.role === "admin"}
						<span
							class="inline-flex items-center gap-1 rounded-full bg-accent-soft px-2 py-0.5 font-mono text-[10px] font-semibold uppercase tracking-[0.08em] text-accent-text"
						>
							<ShieldCheck size={10} aria-hidden="true" />
							Admin
						</span>
					{:else if user?.role}
						<span
							class="inline-flex items-center rounded-full bg-surface px-2 py-0.5 font-mono text-[10px] font-semibold uppercase tracking-[0.08em] text-fg-muted"
						>
							{user.role}
						</span>
					{/if}
				</div>
				<p class="mt-1 truncate font-mono text-[13px] text-fg-muted">
					{user?.email ?? ""}
				</p>
				<p class="mt-1 font-mono text-[11px] text-fg-subtle">
					Member since {formatDateTime(user?.created_at ?? "")}
					{#if authMethodLabel}
						· signs in via {authMethodLabel}
					{/if}
				</p>
			</div>
		</div>

		<dl
			class="grid w-full shrink-0 grid-cols-3 overflow-hidden rounded-lg border border-border bg-surface md:w-auto md:min-w-[320px] md:flex-1 md:max-w-md"
		>
			{@render stat(Monitor, "Sessions", String(sessionCount), true)}
			{@render stat(KeyRound, "API keys", String(keyCount), true)}
			{@render stat(
				Shield,
				"Last seen",
				lastSeen ? formatRelative(lastSeen) : "—",
				false,
			)}
		</dl>
	</div>
</section>

{#snippet stat(
	Icon: typeof Shield,
	label: string,
	value: string,
	withDivider: boolean,
)}
	<div
		class="flex flex-col gap-1 px-3.5 py-3 {withDivider
			? 'border-r border-border'
			: ''}"
	>
		<dt
			class="flex items-center gap-1.5 font-mono text-[9.5px] font-semibold uppercase tracking-[0.14em] text-fg-faint"
		>
			<Icon size={11} aria-hidden="true" />
			{label}
		</dt>
		<dd class="truncate text-base font-semibold tabular-nums text-fg">
			{value}
		</dd>
	</div>
{/snippet}

<style>
	.hero-glow {
		background:
			radial-gradient(
				60% 80% at 20% 10%,
				color-mix(in oklab, var(--accent) 14%, transparent),
				transparent 60%
			),
			radial-gradient(
				40% 60% at 95% 95%,
				color-mix(in oklab, var(--accent) 8%, transparent),
				transparent 70%
			);
	}
</style>
