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
		ChevronDown,
		ListVideo,
		Magnet,
	} from "@lucide/svelte";
	import { isActive as routifyIsActive } from "@roxi/routify";
	import { slide } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { createQuery } from "@tanstack/svelte-query";
	import { api } from "../../lib/api";
	import { NAV_POLL_MS, SILENT } from "../../lib/query";
	import { auth } from "../../lib/auth.svelte";
	import { cn } from "../../lib/cn";
	import {
		TORRENT_PILLS,
		torrentCountsQuery,
		activityCurrent,
		type IsActiveFn,
	} from "../../lib/activity-nav";
	import { navCountsQuery, type NavDot } from "../../lib/nav-counts";
	import type {
		MovieCounts,
		TVShowCounts,
		RequestCounts,
		PendingList,
		SystemInfo,
	} from "../../lib/types";
	import Avatar from "./Avatar.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	const systemQuery = createQuery<SystemInfo>(() => ({
		queryKey: ["system", "info"],
		queryFn: () => api<SystemInfo>("/system/info"),
		meta: SILENT,
		retry: false,
	}));
	let version = $derived(systemQuery.data?.version ?? null);

	let isActiveFn = $state<IsActiveFn>(() => false);
	onMount(() => routifyIsActive.subscribe((fn) => (isActiveFn = fn)));

	const countsQuery = createQuery<MovieCounts>(() => ({
		queryKey: ["movies", "counts"],
		queryFn: () => api<MovieCounts>("/movies/counts"),
		meta: SILENT,
		retry: false,
	}));
	let moviesCount = $derived(countsQuery.data?.total ?? null);

	const seriesCountsQuery = createQuery<TVShowCounts>(() => ({
		queryKey: ["series", "counts"],
		queryFn: () => api<TVShowCounts>("/series/counts"),
		meta: SILENT,
		retry: false,
	}));
	let seriesCount = $derived(seriesCountsQuery.data?.total ?? null);

	const requestCountsQuery = createQuery<RequestCounts>(() => ({
		queryKey: ["requests", "counts"],
		queryFn: () => api<RequestCounts>("/requests/counts"),
		meta: SILENT,
		retry: false,
	}));
	let pendingRequests = $derived(requestCountsQuery.data?.pending ?? 0);

	// Adopted-torrent proposals awaiting an admin decision (shared cache with
	// the activity page's "Needs attention" list).
	const pendingQuery = createQuery<PendingList>(() => ({
		queryKey: ["activity", "pending"],
		queryFn: () => api<PendingList>("/activity/pending"),
		meta: SILENT,
		enabled: auth.isAdmin,
		retry: false,
		refetchInterval: NAV_POLL_MS,
	}));
	let pendingAdoptions = $derived(pendingQuery.data?.items.length ?? 0);

	const libraryItems = [
		{ label: i18n.nav_dashboard(), href: "/", icon: LayoutDashboard },
		{ label: i18n.movies_label(), href: "/movies", icon: Film },
		{ label: i18n.settings_series(), href: "/series", icon: Tv },
	];
	let opsItems = $derived([
		...(auth.isAdmin
			? [{ label: i18n.imports_label(), href: "/library/imports", icon: FolderInput }]
			: []),
		{ label: i18n.common_calendar(), href: "/calendar", icon: CalendarDays },
		{ label: i18n.requests_label(), href: "/requests", icon: Inbox },
	]);

	// Activity covers two routes, so the nav row opens instead of navigating.
	const activityLinks = [
		{ label: i18n.activity_queue_history(), href: "/activity", icon: ListVideo },
		{ label: i18n.torrent_label(), href: "/activity/torrents", icon: Magnet },
	];
	let activityActive = $derived(isActiveFn("/activity"));
	let activityOpen = $state(false);
	// Landing on any activity route unfolds the group so the current page is
	// visible in the nav; leaving collapses it again. A manual toggle in
	// between sticks — the effect only re-runs when the route crosses the
	// activity boundary, not when activityOpen itself changes.
	$effect(() => {
		activityOpen = activityActive;
	});
	const torrentCounts = torrentCountsQuery();
	// Shares the page keys the other nav surfaces already ride, so the pills
	// cost no poll of their own.
	const navCounts = navCountsQuery();

	let torrentTotal = $derived(
		TORRENT_PILLS.reduce(
			(sum, p) => sum + (torrentCounts.counts[p.key] ?? 0),
			0,
		),
	);
	let queueTotal = $derived(
		navCounts.queueDots.reduce((sum, d) => sum + d.count, 0),
	);

	// Folded, the group has to answer for both its routes, so each contributes
	// one pill; unfolded, the rows carry their own per-status ones and repeating
	// them up here would double-count what is already on screen.
	let activityPills = $derived<NavDot[]>(
		[
			{
				key: "queue",
				label: i18n.activity_queue_history(),
				count: queueTotal,
				dot: "downloading",
			},
			{
				key: "torrents",
				label: i18n.torrent_label(),
				count: torrentTotal,
				dot: "seeding",
			},
		].filter((p) => p.count > 0),
	);

	let torrentPills = $derived<NavDot[]>(
		TORRENT_PILLS.map((p) => ({
			key: p.key,
			label: p.key,
			count: torrentCounts.counts[p.key] ?? 0,
			dot: p.dot,
		})).filter((p) => p.count > 0),
	);

	let importPills = $derived<NavDot[]>(
		[
			{
				key: "running",
				label: i18n.lc_running(),
				count: navCounts.imports?.running ?? 0,
				dot: "downloading",
			},
			{
				key: "awaiting_review",
				label: i18n.common_awaiting_review(),
				count: navCounts.imports?.awaiting_review ?? 0,
				dot: "wanted",
			},
		].filter((p) => p.count > 0),
	);

	let roleLabel = $derived.by(() => {
		const r = auth.user?.role;
		if (r === "admin") return "admin";
		if (r === "request_only") return "request";
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

<!-- One vocabulary for every nav count: a status dot and its number, omitted
     when zero, so a settled row stays quiet. Declared before the nav that
     renders it — a snippet is not hoisted, and one defined below its
     {@render} throws "dotPills is not defined" at mount. -->
{#snippet dotPills(pills: NavDot[])}
	{#if pills.length > 0}
		<span
			class="flex shrink-0 items-center gap-1.5 font-mono text-[10px] tabular-nums leading-none text-fg-subtle"
		>
			{#each pills as p (p.key)}
				<span class="flex items-center gap-[3px]" title="{p.count} {p.label}">
					<span
						class="h-[5px] w-[5px] rounded-full"
						style:background-color="var(--status-{p.dot})"
					></span>
					{p.count}
				</span>
			{/each}
		</span>
	{/if}
{/snippet}


<aside
	class="sticky top-0 hidden h-dvh w-64 shrink-0 flex-col gap-3.5 border-r border-border bg-bg-elevated px-3.5 pb-5 pt-5 lg:flex"
	aria-label={i18n.nav_primary_navigation()}
>
	<div class="px-2 pb-2 pt-1">
		<a
			href="/"
			aria-label={i18n.nav_home_dashboard()}
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
		aria-label={i18n.nav_primary()}
		class="flex min-h-0 flex-1 flex-col gap-0.5 overflow-y-auto pr-0.5"
	>
		<div
			class="px-2 pb-1 pt-2 font-mono text-[10px] uppercase tracking-[0.14em] text-fg-faint"
		>
			{i18n.nav_library()}
		</div>
		<ul class="flex flex-col gap-px pb-3">
			{#each libraryItems as item (item.href)}
				{@const active =
					item.href === "/"
						? isActiveFn("/", {}, { recursive: false })
						: isActiveFn(item.href)}
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
			{i18n.nav_operations()}
		</div>
		<ul class="flex flex-col gap-px pb-3">
			<li>
				<button
					type="button"
					onclick={() => (activityOpen = !activityOpen)}
					aria-expanded={activityOpen}
					class={cn(
						itemBase,
						"w-full",
						activityActive ? itemActive : itemInactive,
					)}
				>
					<Activity size={18} class="shrink-0" />
					<span class="flex-1 truncate text-left">{i18n.nav_activity()}</span>
					{#if !activityOpen}
						{@render dotPills(activityPills)}
					{/if}
					<ChevronDown
						size={14}
						class={cn(
							"shrink-0 transition-transform duration-150",
							activityOpen && "rotate-180",
						)}
						aria-hidden="true"
					/>
				</button>
			</li>
			{#if activityOpen}
				<li>
				<ul
					class="flex flex-col gap-px"
					transition:slide={{ duration: 180, easing: cubicOut }}
				>
				{#each activityLinks as link (link.href)}
					{@const current = activityCurrent(isActiveFn, link.href)}
					<li class="ml-[11px] border-l border-border pl-2">
						<a
							href={link.href}
							aria-current={current ? "page" : undefined}
							class={cn(
								"flex items-center gap-2.5 rounded-md px-2.5 py-1.5 text-[13px] transition-colors",
								current
									? "bg-accent-soft font-medium text-accent-text"
									: "text-fg-muted hover:bg-surface hover:text-fg",
							)}
						>
							<link.icon size={15} class="shrink-0" />
							<span class="min-w-0 flex-1 truncate">{link.label}</span>
							{#if link.href === "/activity"}
								{#if pendingAdoptions > 0}
									<span
										class="shrink-0 rounded-full bg-status-wanted/20 px-1.5 py-px font-mono text-[10.5px] tabular-nums text-status-wanted"
										title={i18n.nav_adopted_needs_attention()}
									>
										{pendingAdoptions.toLocaleString()}
									</span>
								{/if}
								{@render dotPills(navCounts.queueDots)}
							{/if}
							{#if link.href === "/activity/torrents"}
								{@render dotPills(torrentPills)}
							{/if}
						</a>
					</li>
				{/each}
				</ul>
				</li>
			{/if}
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
						{#if item.href === "/library/imports"}
							{@render dotPills(importPills)}
						{/if}
						{#if item.href === "/requests" && pendingRequests > 0}
							<span
								class="shrink-0 rounded-full bg-status-wanted/20 px-1.5 py-px font-mono text-[10.5px] tabular-nums text-status-wanted"
							>
								{pendingRequests.toLocaleString()}
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
				<span class="flex-1 truncate">{i18n.nav_settings()}</span>
			</a>
		{/if}

		{#if auth.user}
			{@const accountActive = isActiveFn("/account")}
			<div class="flex items-center gap-1">
				<a
					href="/account"
					aria-current={accountActive ? "page" : undefined}
					aria-label={i18n.nav_account_settings()}
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
							{roleLabel}{version ? ` · ${version}` : ""}
						</div>
					</div>
				</a>
				<button
					type="button"
					onclick={signOut}
					aria-label={i18n.common_sign_out()}
					title={i18n.common_sign_out()}
					class="grid h-10 w-10 shrink-0 place-items-center rounded-md text-fg-muted transition-colors hover:bg-status-failed/10 hover:text-status-failed"
				>
					<LogOut size={18} aria-hidden="true" />
				</button>
			</div>
		{/if}
	</div>
</aside>