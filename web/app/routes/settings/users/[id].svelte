<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import { params } from "@roxi/routify";
	import { onMount } from "svelte";
	import { ArrowLeft } from "@lucide/svelte";
	import { api } from "../../../lib/api";
	import { auth } from "../../../lib/auth.svelte";
	import { requireAdmin } from "../../../lib/guards";
	import type { UserDetail } from "../../../lib/types";
	import UserHero from "../../../components/users/UserHero.svelte";
	import UserDetailHeader from "../../../components/users/UserDetailHeader.svelte";
	import UserDangerActions from "../../../components/users/UserDangerActions.svelte";
	import UserSessionsCard from "../../../components/users/UserSessionsCard.svelte";
	import UserAPIKeysCard from "../../../components/users/UserAPIKeysCard.svelte";

	let routeParams = $state<Record<string, string>>({});
	onMount(() => {
		const unsub = params.subscribe((p) => (routeParams = p));
		return unsub;
	});

	$effect(() => {
		if (!auth.loading) requireAdmin();
	});

	const userId = $derived(Number(routeParams.id));

	const detail = createQuery<UserDetail>(() => ({
		queryKey: ["user", userId],
		queryFn: () => api<UserDetail>(`/users/${userId}`),
		enabled:
			Number.isFinite(userId) &&
			userId > 0 &&
			!auth.loading &&
			auth.user?.role === "admin",
	}));

	let target = $derived(detail.data?.user);
	let isSelf = $derived(target?.id === auth.user?.id);
</script>

<div class="mx-auto w-full max-w-5xl px-4 py-6 md:px-8 md:py-7">
	<a
		href="/settings/users"
		class="inline-flex items-center gap-1.5 rounded-md border border-border bg-bg-elevated px-3 py-1.5 text-xs font-medium text-fg-muted transition hover:border-border-strong hover:text-fg"
	>
		<ArrowLeft class="h-3.5 w-3.5" aria-hidden="true" />
		Back to Users
	</a>

	{#if detail.isPending}
		<div class="mt-6 py-16 text-center text-sm text-fg-subtle">
			Loading user…
		</div>
	{:else if detail.isError}
		<div
			class="mt-6 rounded-lg border border-dashed border-status-failed/40 bg-status-failed/5 py-12 text-center"
		>
			<p class="text-sm font-semibold text-status-failed">
				Failed to load user
			</p>
			<p class="mt-1 text-xs text-fg-subtle">
				{detail.error?.message ?? "Unknown error"}
			</p>
		</div>
	{:else if target && detail.data}
		<div class="mt-4">
			<UserHero
				user={target}
				apiKeys={detail.data.api_keys}
				sessions={detail.data.sessions}
			/>
		</div>

		{@render section("Identity", "Profile fields applied immediately.")}
		<div class="grid items-start gap-5">
			<UserDetailHeader user={target} />
		</div>

		{@render section(
			"Devices & access",
			"Where this user is signed in and which tools can talk to their account.",
		)}
		<div class="grid items-start gap-5 md:grid-cols-2">
			<UserAPIKeysCard
				userId={target.id}
				apiKeys={detail.data.api_keys}
			/>
			<UserSessionsCard
				userId={target.id}
				sessions={detail.data.sessions}
			/>
		</div>

		{@render section(
			"Danger zone",
			"Destructive actions on this account. Admin only.",
			true,
		)}
		<UserDangerActions user={target} {isSelf} />
	{/if}
</div>

{#snippet section(title: string, subtitle: string, danger = false)}
	<header class="mb-4 mt-10 flex items-baseline gap-3">
		<h2
			class="text-[11px] font-semibold uppercase tracking-[0.14em] {danger
				? 'text-status-failed'
				: 'text-fg-faint'}"
		>
			{title}
		</h2>
		<span class="h-px flex-1 bg-border" aria-hidden="true"></span>
		<p class="text-xs text-fg-subtle">{subtitle}</p>
	</header>
{/snippet}
