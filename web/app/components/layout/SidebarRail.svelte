<script lang="ts">
	import { onMount } from "svelte";
	import { fly } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import {
		LayoutDashboard,
		Library,
		Film,
		Tv,
		Activity,
		ListVideo,
		Magnet,
		CalendarDays,
		Inbox,
		FolderInput,
		Settings,
		LogOut,
	} from "@lucide/svelte";
	import { isActive as routifyIsActive } from "@roxi/routify";
	import { createQuery } from "@tanstack/svelte-query";
	import { api } from "../../lib/api";
	import type { RequestCounts } from "../../lib/types";
	import { auth } from "../../lib/auth.svelte";
	import { cn } from "../../lib/cn";
	import {
		TORRENT_PILLS,
		torrentCountsQuery,
		activityCurrent,
		type IsActiveFn,
	} from "../../lib/activity-nav";
	import { navCountsQuery } from "../../lib/nav-counts";
	import Avatar from "./Avatar.svelte";

	// Tablet band (md → lg): the desktop sidebar collapsed to icons, rather than
	// the phone's four-cell bar. Every top-level destination is on screen, so
	// nothing hides behind a More sheet, and Library can keep growing without
	// running out of cells.
	let isActiveFn = $state<IsActiveFn>(() => false);
	onMount(() => routifyIsActive.subscribe((fn) => (isActiveFn = fn)));

	const counts = navCountsQuery();
	const torrentCounts = torrentCountsQuery();

	const requestCountsQuery = createQuery<RequestCounts>(() => ({
		queryKey: ["requests", "counts"],
		queryFn: () => api<RequestCounts>("/requests/counts"),
		retry: false,
	}));
	let pendingRequests = $derived(requestCountsQuery.data?.pending ?? 0);

	type Link = { label: string; href: string; icon: typeof Tv };

	const MENUS: Record<string, Link[]> = {
		Library: [
			{ label: "Movies", href: "/movies", icon: Film },
			{ label: "Series", href: "/series", icon: Tv },
		],
		Activity: [
			{ label: "Queue & History", href: "/activity", icon: ListVideo },
			{ label: "Torrents", href: "/activity/torrents", icon: Magnet },
		],
	};

	let flyout = $state("");
	function closeFlyout() {
		flyout = "";
	}
	// Outside-click / Escape, without binding a ref inside the {#each}.
	function popover(node: HTMLElement) {
		const onDoc = (e: MouseEvent) => {
			if (!node.parentElement?.contains(e.target as Node)) closeFlyout();
		};
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") closeFlyout();
		};
		const t = setTimeout(() => document.addEventListener("mousedown", onDoc));
		document.addEventListener("keydown", onKey);
		return {
			destroy() {
				clearTimeout(t);
				document.removeEventListener("mousedown", onDoc);
				document.removeEventListener("keydown", onKey);
			},
		};
	}

	let libraryActive = $derived(["/movies", "/series"].some((p) => isActiveFn(p)));
	let activityActive = $derived(isActiveFn("/activity"));
	let dashActive = $derived(isActiveFn("/dashboard"));

	// Dot badges ride the icon: the rail has no room for a number, but it can
	// say "something here wants you". Requests is pending approvals; Imports is
	// a scan waiting on a review decision, or one still running.
	let opsItems = $derived([
		{ label: "Calendar", href: "/calendar", icon: CalendarDays, dot: null as string | null },
		{
			label: "Requests",
			href: "/requests",
			icon: Inbox,
			dot: pendingRequests > 0 ? "wanted" : null,
		},
		...(auth.isAdmin
			? [
					{
						label: "Imports",
						href: "/library/imports",
						icon: FolderInput,
						dot: counts.importsDot,
					},
				]
			: []),
		...(auth.isAdmin
			? [{ label: "Settings", href: "/settings", icon: Settings, dot: null }]
			: []),
	]);

	async function signOut() {
		try {
			await fetch("/auth/logout", { method: "POST", credentials: "same-origin" });
		} finally {
			window.location.href = "/login";
		}
	}

	const itemBase =
		"relative flex w-[68px] flex-col items-center gap-1.5 rounded-xl px-1 py-2.5 text-[10.5px] font-medium transition-colors";
	const itemInactive = "text-fg-subtle hover:bg-surface hover:text-fg";
	const itemActive =
		"bg-accent-soft text-accent-text before:absolute before:-left-2.5 before:top-1/2 before:h-5 before:w-[3px] before:-translate-y-1/2 before:rounded-r-full before:bg-accent";
	const menuRow =
		"flex items-center gap-3 rounded-lg px-3 py-2.5 text-[13.5px] font-medium transition-colors";
</script>

<aside
	class="sticky top-0 z-40 hidden h-dvh w-[88px] shrink-0 flex-col items-center gap-1 border-r border-border bg-bg-elevated px-2.5 pb-4 pt-4 md:flex lg:hidden"
	aria-label="Primary navigation"
>
	<a
		href="/dashboard"
		aria-label="Streamline home — go to dashboard"
		class="mb-3 rounded-md transition hover:opacity-90"
	>
		<img
			src="/static/images/favicon-512.png"
			alt=""
			class="h-9 w-9 rounded-md object-cover shadow-sm ring-1 ring-border"
		/>
	</a>

	<nav aria-label="Primary" class="flex flex-col items-center gap-1">
		<a
			href="/dashboard"
			aria-current={dashActive ? "page" : undefined}
			class={cn(itemBase, dashActive ? itemActive : itemInactive)}
		>
			<LayoutDashboard size={20} strokeWidth={dashActive ? 2 : 1.6} />
			<span>Dashboard</span>
		</a>

		{#each [{ label: "Library", icon: Library, active: libraryActive }, { label: "Activity", icon: Activity, active: activityActive }] as group (group.label)}
			{@const on = group.active || flyout === group.label}
			<div class="relative flex justify-center">
				<button
					type="button"
					onclick={() => (flyout = flyout === group.label ? "" : group.label)}
					aria-haspopup="menu"
					aria-expanded={flyout === group.label}
					class={cn(itemBase, on ? itemActive : itemInactive)}
				>
					<group.icon size={20} strokeWidth={on ? 2 : 1.6} />
					<span>{group.label}</span>
				</button>

				{#if flyout === group.label}
					<div
						use:popover
						role="menu"
						aria-label={group.label}
						transition:fly={{ x: -8, duration: 160, easing: cubicOut }}
						onclick={closeFlyout}
						class="absolute left-full top-0 z-50 ml-2.5 w-[272px] overflow-hidden rounded-xl border border-border-strong bg-bg-elevated p-1.5 shadow-4"
					>
						<div
							class="flex items-center justify-between px-2.5 pb-1.5 pt-1 font-mono text-[9.5px] uppercase tracking-[0.16em] text-fg-faint"
						>
							<span>{group.label}</span>
							{#if group.label === "Activity"}
								<span class="flex items-center gap-1.5 tracking-[0.08em]">
									<span class="h-[5px] w-[5px] rounded-full bg-status-available"></span>
									live
								</span>
							{/if}
						</div>
						{#each MENUS[group.label] ?? [] as link (link.href)}
							{@const current =
								group.label === "Activity"
									? activityCurrent(isActiveFn, link.href)
									: isActiveFn(link.href)}
							<a
								href={link.href}
								role="menuitem"
								aria-current={current ? "page" : undefined}
								class={cn(
									menuRow,
									current
										? "bg-accent-soft text-accent-text"
										: "text-fg-muted hover:bg-surface hover:text-fg",
								)}
							>
								<link.icon size={18} class="shrink-0" />
								<span class="min-w-0 flex-1 truncate">{link.label}</span>
								{#if link.href === "/movies" && counts.moviesTotal !== null}
									<span
										class={cn(
											"shrink-0 font-mono text-[11px] tabular-nums",
											current ? "text-accent-text opacity-70" : "text-fg-faint",
										)}
									>
										{counts.moviesTotal.toLocaleString()}
									</span>
								{:else if link.href === "/series" && counts.seriesTotal !== null}
									<span
										class={cn(
											"shrink-0 font-mono text-[11px] tabular-nums",
											current ? "text-accent-text opacity-70" : "text-fg-faint",
										)}
									>
										{counts.seriesTotal.toLocaleString()}
									</span>
								{:else if link.href === "/activity/torrents"}
									<span
										class="flex shrink-0 items-center gap-1.5 font-mono text-[10px] leading-none tabular-nums text-fg-subtle"
									>
										{#each TORRENT_PILLS as p (p.key)}
											{#if (torrentCounts.counts[p.key] ?? 0) > 0}
												<span
													class="flex items-center gap-[3px]"
													title="{torrentCounts.counts[p.key]} {p.key}"
												>
													<span
														class="h-[5px] w-[5px] rounded-full"
														style:background-color="var(--status-{p.dot})"
													></span>
													{torrentCounts.counts[p.key]}
												</span>
											{/if}
										{/each}
									</span>
								{/if}
							</a>
						{/each}
					</div>
				{/if}
			</div>
		{/each}

		{#each opsItems as item (item.href)}
			{@const active = isActiveFn(item.href)}
			<a
				href={item.href}
				aria-current={active ? "page" : undefined}
				class={cn(itemBase, active ? itemActive : itemInactive)}
			>
				<span class="relative">
					<item.icon size={20} strokeWidth={active ? 2 : 1.6} />
					{#if item.dot}
						<span
							class="absolute -right-1.5 -top-1 h-[7px] w-[7px] rounded-full ring-2 ring-bg-elevated"
							style:background-color="var(--status-{item.dot})"
							aria-hidden="true"
						></span>
					{/if}
				</span>
				<span>{item.label}</span>
			</a>
		{/each}
	</nav>

	<div class="flex-1"></div>

	{#if auth.user}
		{@const accountActive = isActiveFn("/account")}
		<div class="flex w-full flex-col items-center gap-1 border-t border-border pt-3">
			<a
				href="/account"
				aria-current={accountActive ? "page" : undefined}
				aria-label="Account settings"
				title={auth.user.display_name || auth.user.email}
				class={cn(
					"grid h-12 w-12 place-items-center rounded-xl transition-colors",
					accountActive ? "bg-accent-soft" : "hover:bg-surface",
				)}
			>
				<Avatar email={auth.user.email} name={auth.user.display_name} size={34} />
			</a>
			<button
				type="button"
				onclick={signOut}
				aria-label="Sign out"
				title="Sign out"
				class="grid h-11 w-11 place-items-center rounded-xl text-fg-muted transition-colors hover:bg-status-failed/10 hover:text-status-failed"
			>
				<LogOut size={19} aria-hidden="true" />
			</button>
		</div>
	{/if}
</aside>
