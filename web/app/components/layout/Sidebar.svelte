<script lang="ts">
	import { onMount } from "svelte";
	import {
		LayoutDashboard,
		Film,
		Tv,
		Activity,
		FolderInput,
		CalendarDays,
		Inbox,
		Settings,
		LogOut,
	} from "@lucide/svelte";
	import { isActive as routifyIsActive } from "@roxi/routify";
	import { createQuery } from "@tanstack/svelte-query";
	import { api } from "../../lib/api";
	import { auth } from "../../lib/auth.svelte";
	import { cn } from "../../lib/cn";
	import type {
		MovieCounts,
		TVShowCounts,
		RequestCounts,
		PendingList,
	} from "../../lib/types";
	import Avatar from "./Avatar.svelte";

	const VERSION = "dev";

	type IsActiveFn = (path: string) => boolean;
	let isActiveFn = $state<IsActiveFn>(() => false);
	onMount(() => routifyIsActive.subscribe((fn) => (isActiveFn = fn)));

	const countsQuery = createQuery<MovieCounts>(() => ({
		queryKey: ["movies", "counts"],
		queryFn: () => api<MovieCounts>("/movies/counts"),
		retry: false,
	}));
	let moviesCount = $derived(countsQuery.data?.total ?? null);

	const seriesCountsQuery = createQuery<TVShowCounts>(() => ({
		queryKey: ["series", "counts"],
		queryFn: () => api<TVShowCounts>("/series/counts"),
		retry: false,
	}));
	let seriesCount = $derived(seriesCountsQuery.data?.total ?? null);

	const requestCountsQuery = createQuery<RequestCounts>(() => ({
		queryKey: ["requests", "counts"],
		queryFn: () => api<RequestCounts>("/requests/counts"),
		retry: false,
	}));
	let pendingRequests = $derived(requestCountsQuery.data?.pending ?? 0);

	// Adopted-torrent proposals awaiting an admin decision (shared cache with
	// the activity page's "Needs attention" list).
	const pendingQuery = createQuery<PendingList>(() => ({
		queryKey: ["activity", "pending"],
		queryFn: () => api<PendingList>("/activity/pending"),
		enabled: auth.isAdmin,
		retry: false,
		refetchInterval: 30000,
	}));
	let pendingAdoptions = $derived(pendingQuery.data?.items.length ?? 0);

	const libraryItems = [
		{ label: "Dashboard", href: "/dashboard", icon: LayoutDashboard },
		{ label: "Movies", href: "/movies", icon: Film },
		{ label: "Series", href: "/series", icon: Tv },
	];
	let opsItems = $derived([
		{ label: "Activity", href: "/activity", icon: Activity },
		...(auth.isAdmin
			? [{ label: "Imports", href: "/library/imports", icon: FolderInput }]
			: []),
		{ label: "Calendar", href: "/calendar", icon: CalendarDays },
		{ label: "Requests", href: "/requests", icon: Inbox },
	]);

	let roleLabel = $derived.by(() => {
		const r = auth.user?.role;
		if (r === "admin") return "admin";
		if (r === "request_only") return "request only";
		return "member";
	});

	async function signOut() {
		try {
			await fetch("/auth/logout", {
				method: "POST",
				credentials: "same-origin",
			});
		} finally {
			window.location.href = "/login";
		}
	}

	const itemBase =
		"group relative flex items-center gap-3 overflow-hidden rounded-md px-2.5 py-2 text-[13.5px] font-medium transition-colors duration-150";
	const itemInactive =
		"text-fg-muted hover:bg-surface hover:text-fg";
	const itemActive =
		"bg-accent-soft text-accent-text before:absolute before:-left-3.5 before:top-1/2 before:-translate-y-1/2 before:h-4 before:w-[3px] before:rounded-r-full before:bg-accent";

</script>

<aside
	class="sticky top-0 hidden h-dvh w-64 shrink-0 flex-col gap-3.5 border-r border-border bg-bg-elevated px-3.5 pb-5 pt-5 lg:flex"
	aria-label="Primary navigation"
>
	<div class="px-2 pb-2 pt-1">
		<a
			href="/dashboard"
			aria-label="Streamline home — go to dashboard"
			class="flex items-center gap-3 rounded-md transition hover:opacity-90"
		>
			<img
				src="/static/images/favicon-512.png"
				alt=""
				class="h-9 w-9 rounded-md object-cover shadow-sm ring-1 ring-border"
			/>
			<div class="min-w-0">
				<div class="text-[15px] font-semibold leading-tight tracking-tight text-fg">
					streamline
				</div>
				<div class="mt-px font-mono text-[10px] uppercase tracking-[0.14em] text-fg-faint">
					cinematic ops
				</div>
			</div>
		</a>
	</div>

	<nav
		aria-label="Primary"
		class="flex min-h-0 flex-1 flex-col gap-0.5 overflow-y-auto pr-0.5"
	>
		<div
			class="px-2 pb-1 pt-2 font-mono text-[10px] uppercase tracking-[0.14em] text-fg-faint"
		>
			Library
		</div>
		<ul class="flex flex-col gap-px pb-3">
			{#each libraryItems as item (item.href)}
				{@const active = isActiveFn(item.href)}
				<li>
					<a
						href={item.href}
						aria-current={active ? "page" : undefined}
						class={cn(itemBase, active ? itemActive : itemInactive)}
					>
						<item.icon size={18} class="shrink-0" />
						<span class="flex-1 truncate">{item.label}</span>
						{#if item.href === "/movies" && moviesCount !== null}
							<span
								class={cn(
									"shrink-0 font-mono text-[10.5px] tabular-nums",
									active ? "text-accent-text opacity-70" : "text-fg-faint",
								)}
							>
								{moviesCount.toLocaleString()}
							</span>
						{:else if item.href === "/series" && seriesCount !== null}
							<span
								class={cn(
									"shrink-0 font-mono text-[10.5px] tabular-nums",
									active ? "text-accent-text opacity-70" : "text-fg-faint",
								)}
							>
								{seriesCount.toLocaleString()}
							</span>
						{/if}
					</a>
				</li>
			{/each}
		</ul>

		<div
			class="px-2 pb-1 pt-2 font-mono text-[10px] uppercase tracking-[0.14em] text-fg-faint"
		>
			Operations
		</div>
		<ul class="flex flex-col gap-px pb-3">
			{#each opsItems as item (item.href)}
				{@const active = isActiveFn(item.href)}
				<li>
					<a
						href={item.href}
						aria-current={active ? "page" : undefined}
						class={cn(itemBase, active ? itemActive : itemInactive)}
					>
						<item.icon size={18} class="shrink-0" />
						<span class="flex-1 truncate">{item.label}</span>
						{#if item.href === "/requests" && pendingRequests > 0}
							<span
								class="shrink-0 rounded-full bg-status-wanted/20 px-1.5 py-px font-mono text-[10.5px] tabular-nums text-status-wanted"
							>
								{pendingRequests.toLocaleString()}
							</span>
						{:else if item.href === "/activity" && pendingAdoptions > 0}
							<span
								class="shrink-0 rounded-full bg-status-wanted/20 px-1.5 py-px font-mono text-[10.5px] tabular-nums text-status-wanted"
								title="Adopted torrents need attention"
							>
								{pendingAdoptions.toLocaleString()}
							</span>
						{/if}
					</a>
				</li>
			{/each}
		</ul>
	</nav>

	<div class="flex flex-col gap-2 border-t border-border pt-3">
		{#if auth.isAdmin}
			<a
				href="/settings"
				aria-current={isActiveFn("/settings") ? "page" : undefined}
				class={cn(itemBase, isActiveFn("/settings") ? itemActive : itemInactive)}
			>
				<Settings size={18} class="shrink-0" />
				<span class="flex-1 truncate">Settings</span>
			</a>
		{/if}

		{#if auth.user}
			{@const accountActive = isActiveFn("/account")}
			<div class="flex items-center gap-1">
				<a
					href="/account"
					aria-current={accountActive ? "page" : undefined}
					aria-label="Account settings"
					class={cn(
						"flex min-w-0 flex-1 items-center gap-2.5 rounded-md px-2 py-1.5 transition-colors",
						accountActive
							? "bg-accent-soft text-accent-text"
							: "text-fg-muted hover:bg-surface hover:text-fg",
					)}
				>
					<Avatar
						email={auth.user.email}
						name={auth.user.display_name}
						size={32}
					/>
					<div class="min-w-0 flex-1">
						<div class="truncate text-[13px] font-medium leading-tight">
							{auth.user.display_name || auth.user.email}
						</div>
						<div class="mt-px truncate font-mono text-[10px] text-fg-faint">
							{roleLabel} · v{VERSION}
						</div>
					</div>
				</a>
				<button
					type="button"
					onclick={signOut}
					aria-label="Sign out"
					title="Sign out"
					class="grid h-10 w-10 shrink-0 place-items-center rounded-md text-fg-muted transition-colors hover:bg-status-failed/10 hover:text-status-failed"
				>
					<LogOut size={18} aria-hidden="true" />
				</button>
			</div>
		{/if}
	</div>
</aside>
