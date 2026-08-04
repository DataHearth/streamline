<script lang="ts">
	import { onMount } from "svelte";
	import { fly, fade } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import {
		LayoutDashboard,
		Library,
		Film,
		Activity,
		ListVideo,
		Magnet,
		Tv,
		CalendarDays,
		Inbox,
		FolderInput,
		Settings,
		LogOut,
		MoreHorizontal,
		X,
	} from "@lucide/svelte";
	import { isActive as routifyIsActive } from "@roxi/routify";
	import { createQuery } from "@tanstack/svelte-query";
	import { api } from "../../lib/api";
	import type { RequestCounts, SystemInfo } from "../../lib/types";
	import { auth } from "../../lib/auth.svelte";
	import { cn } from "../../lib/cn";
	import {
		TORRENT_PILLS,
		torrentCountsQuery,
		activityCurrent,
		type IsActiveFn,
	} from "../../lib/activity-nav";
	import Avatar from "./Avatar.svelte";

	let isActiveFn = $state<IsActiveFn>(() => false);
	onMount(() => routifyIsActive.subscribe((fn) => (isActiveFn = fn)));

	// Primary bar — always four cells so the grid is identical across roles.
	const primary = [
		{ label: "Dashboard", href: "/dashboard", icon: LayoutDashboard },
		{ label: "Library", icon: Library, expand: true },
		{ label: "Activity", icon: Activity, expand: true },
	];

	// Everything the sidebar exposes on desktop but the bar can't hold, grouped
	// the same way for a coherent mental model.
	type SheetItem = { label: string; href: string; icon: typeof Tv };
	let opsItems = $derived<SheetItem[]>([
		{ label: "Calendar", href: "/calendar", icon: CalendarDays },
		{ label: "Requests", href: "/requests", icon: Inbox },
		...(auth.isAdmin
			? [{ label: "Imports", href: "/library/imports", icon: FolderInput }]
			: []),
	]);
	let adminItems = $derived<SheetItem[]>(
		auth.isAdmin ? [{ label: "Settings", href: "/settings", icon: Settings }] : [],
	);

	// Secondary routes light up the "More" cell so the user still has a sense of
	// place while on a sheet-reached page.
	const SECONDARY = [
		"/calendar",
		"/requests",
		"/library/imports",
		"/settings",
		"/account",
	];
	let moreActive = $derived(SECONDARY.some((p) => isActiveFn(p)));

	// Expanding bar cells (mobile): one generic cell fans out to its real
	// destinations. Activity is two destinations across two routes.
	const MENUS: Record<string, SheetItem[]> = {
		Library: [
			{ label: "Movies", href: "/movies", icon: Film },
			{ label: "Series", href: "/series", icon: Tv },
		],
		Activity: [
			{ label: "Queue & History", href: "/activity", icon: ListVideo },
			{ label: "Torrents", href: "/activity/torrents", icon: Magnet },
		],
	};
	let menuOpen = $state("");
	const torrentCounts = torrentCountsQuery();

	function menuActive(label: string) {
		return (MENUS[label] ?? []).some((l) => isActiveFn(l.href));
	}
	function closeMenu() {
		menuOpen = "";
	}
	// Outside-click / Escape, without binding a ref inside the {#each}.
	function menuPopover(node: HTMLElement) {
		const onDoc = (e: MouseEvent) => {
			if (!node.parentElement?.contains(e.target as Node)) closeMenu();
		};
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") closeMenu();
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

	const requestCountsQuery = createQuery<RequestCounts>(() => ({
		queryKey: ["requests", "counts"],
		queryFn: () => api<RequestCounts>("/requests/counts"),
		retry: false,
	}));
	let pendingRequests = $derived(requestCountsQuery.data?.pending ?? 0);

	const systemQuery = createQuery<SystemInfo>(() => ({
		queryKey: ["system", "info"],
		queryFn: () => api<SystemInfo>("/system/info"),
		retry: false,
	}));
	let version = $derived(systemQuery.data?.version ?? null);

	let roleLabel = $derived.by(() => {
		const r = auth.user?.role;
		if (r === "admin") return "admin";
		if (r === "request_only") return "request";
		return "member";
	});

	let open = $state(false);
	function openSheet() {
		open = true;
	}
	function closeSheet() {
		open = false;
	}

	// Routify intercepts internal <a> clicks and navigates client-side; close
	// the sheet on any click inside it so a tapped link doesn't leave the
	// overlay hanging over the new page.
	function onSheetClick(e: MouseEvent) {
		if ((e.target as HTMLElement).closest("a")) closeSheet();
	}

	$effect(() => {
		if (!open) return;
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") closeSheet();
		};
		document.addEventListener("keydown", onKey);
		return () => document.removeEventListener("keydown", onKey);
	});

	async function signOut() {
		closeSheet();
		try {
			await fetch("/auth/logout", { method: "POST", credentials: "same-origin" });
		} finally {
			window.location.href = "/login";
		}
	}

	const rowBase =
		"flex items-center gap-3 rounded-lg px-3 py-3 text-[14px] font-medium transition-colors";
	const rowInactive = "text-fg-muted hover:bg-surface hover:text-fg";
	const rowActive =
		"bg-accent-soft text-accent-text before:absolute before:left-0 before:top-1/2 before:h-5 before:w-[3px] before:-translate-y-1/2 before:rounded-r-full before:bg-accent";
</script>

<nav
	class="fixed inset-x-0 bottom-0 z-40 grid grid-cols-4 min-h-14 border-t border-border bg-bg-elevated/95 pb-[env(safe-area-inset-bottom)] backdrop-blur-md saturate-150 lg:hidden"
	aria-label="Primary"
>
	{#each primary as tab (tab.label)}
		{#if tab.expand}
			<div class="relative flex min-w-0">
				<button
					type="button"
					onclick={() => (menuOpen = menuOpen === tab.label ? "" : tab.label)}
					aria-haspopup="menu"
					aria-expanded={menuOpen === tab.label}
					aria-label={tab.label}
					class={cn(
						"relative flex w-full flex-col items-center justify-center gap-1 px-2 pt-2.5 pb-3 text-[10.5px] transition-colors",
						menuActive(tab.label) || menuOpen === tab.label
							? "text-accent-text before:absolute before:inset-x-[18%] before:top-0 before:h-0.5 before:rounded-b-sm before:bg-accent"
							: "text-fg-subtle hover:text-fg-muted",
					)}
				>
					<tab.icon
						size={20}
						strokeWidth={menuActive(tab.label) || menuOpen === tab.label
							? 2
							: 1.6}
					/>
					<span>{tab.label}</span>
				</button>
				{#if menuOpen === tab.label}
					<div
						use:menuPopover
						role="menu"
						aria-label={tab.label}
						transition:fly={{ y: 8, duration: 160, easing: cubicOut }}
						class="absolute bottom-full left-1/2 z-50 mb-2 w-56 -translate-x-1/2 overflow-hidden rounded-xl border border-border-strong bg-bg-elevated p-1 shadow-4"
					>
						{#each MENUS[tab.label] ?? [] as link (link.href)}
							{@const current = activityCurrent(isActiveFn, link.href)}
							<a
								href={link.href}
								role="menuitem"
								onclick={closeMenu}
								aria-current={current ? "page" : undefined}
								class={cn(
									"flex items-center gap-2.5 rounded-lg px-3 py-2.5 text-[13.5px] font-medium transition-colors",
									current
										? "bg-accent-soft text-accent-text"
										: "text-fg-muted hover:bg-surface hover:text-fg",
								)}
							>
								<link.icon size={18} class="shrink-0" />
								<span class="min-w-0 flex-1">{link.label}</span>
								{#if link.href === "/activity/torrents"}
									<span
										class="flex shrink-0 items-center gap-1.5 font-mono text-[10px] tabular-nums leading-none text-fg-subtle"
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
		{:else}
			{@const active = isActiveFn(tab.href ?? "")}
			<a
				href={tab.href}
				aria-current={active ? "page" : undefined}
				class={cn(
					"relative flex flex-col items-center justify-center gap-1 px-2 pt-2.5 pb-3 text-[10.5px] transition-colors",
					active
						? "text-accent-text before:absolute before:inset-x-[18%] before:top-0 before:h-0.5 before:rounded-b-sm before:bg-accent"
						: "text-fg-subtle hover:text-fg-muted",
				)}
			>
				<tab.icon size={20} strokeWidth={active ? 2 : 1.6} />
				<span>{tab.label}</span>
			</a>
		{/if}
	{/each}

	<button
		type="button"
		onclick={openSheet}
		aria-haspopup="dialog"
		aria-expanded={open}
		aria-label="More destinations"
		class={cn(
			"relative flex flex-col items-center justify-center gap-1 px-2 pt-2.5 pb-3 text-[10.5px] transition-colors",
			moreActive || open
				? "text-accent-text before:absolute before:inset-x-[18%] before:top-0 before:h-0.5 before:rounded-b-sm before:bg-accent"
				: "text-fg-subtle hover:text-fg-muted",
		)}
	>
		<div class="relative">
			<MoreHorizontal size={20} strokeWidth={moreActive || open ? 2 : 1.6} />
			{#if pendingRequests > 0 && !open}
				<span
					class="absolute -right-1.5 -top-1 h-1.5 w-1.5 rounded-full bg-status-wanted"
					aria-hidden="true"
				></span>
			{/if}
		</div>
		<span>More</span>
	</button>
</nav>

{#if open}
	<div class="fixed inset-0 z-50 lg:hidden" role="dialog" aria-modal="true" aria-label="More">
		<button
			type="button"
			aria-label="Close menu"
			transition:fade={{ duration: 180 }}
			onclick={closeSheet}
			class="absolute inset-0 h-full w-full cursor-default bg-black/55 backdrop-blur-[2px]"
		></button>

		<div
			transition:fly={{ y: 420, duration: 300, easing: cubicOut }}
			onclick={onSheetClick}
			class="absolute inset-x-0 bottom-0 flex max-h-[85dvh] flex-col overflow-hidden rounded-t-2xl border-t border-border-strong bg-bg-elevated shadow-4"
		>
			<div class="flex items-center justify-between px-5 pb-1 pt-3">
				<span
					aria-hidden="true"
					class="absolute left-1/2 top-2 h-1 w-9 -translate-x-1/2 rounded-full bg-border-strong"
				></span>
				<span
					class="mt-2 font-mono text-[10px] uppercase tracking-[0.18em] text-fg-faint"
				>
					Navigate
				</span>
				<button
					type="button"
					onclick={closeSheet}
					aria-label="Close"
					class="mt-1 grid h-9 w-9 place-items-center rounded-md text-fg-subtle transition hover:bg-surface hover:text-fg"
				>
					<X size={18} aria-hidden="true" />
				</button>
			</div>

			<div class="min-h-0 flex-1 overflow-y-auto px-3 pb-[max(env(safe-area-inset-bottom),12px)]">
				<div class="px-2 pb-1 pt-2 font-mono text-[10px] uppercase tracking-[0.14em] text-fg-faint">
					Operations
				</div>
				<ul class="flex flex-col gap-0.5">
					{#each opsItems as item (item.href)}
						{@const active = isActiveFn(item.href)}
						<li class="relative">
							<a
								href={item.href}
								aria-current={active ? "page" : undefined}
								class={cn(rowBase, active ? rowActive : rowInactive)}
							>
								<item.icon size={19} class="shrink-0" />
								<span class="flex-1 truncate">{item.label}</span>
								{#if item.href === "/requests" && pendingRequests > 0}
									<span
										class="shrink-0 rounded-full bg-status-wanted/20 px-2 py-0.5 font-mono text-[11px] tabular-nums text-status-wanted"
									>
										{pendingRequests.toLocaleString()}
									</span>
								{/if}
							</a>
						</li>
					{/each}
				</ul>

				{#if adminItems.length}
					<div class="px-2 pb-1 pt-3 font-mono text-[10px] uppercase tracking-[0.14em] text-fg-faint">
						Admin
					</div>
					<ul class="flex flex-col gap-0.5">
						{#each adminItems as item (item.href)}
							{@const active = isActiveFn(item.href)}
							<li class="relative">
								<a
									href={item.href}
									aria-current={active ? "page" : undefined}
									class={cn(rowBase, active ? rowActive : rowInactive)}
								>
									<item.icon size={19} class="shrink-0" />
									<span class="flex-1 truncate">{item.label}</span>
								</a>
							</li>
						{/each}
					</ul>
				{/if}

				{#if auth.user}
					<div class="mt-3 flex items-center gap-2 border-t border-border pt-3">
						<a
							href="/account"
							aria-current={isActiveFn("/account") ? "page" : undefined}
							class={cn(
								"flex min-w-0 flex-1 items-center gap-3 rounded-lg px-2 py-2 transition-colors",
								isActiveFn("/account")
									? "bg-accent-soft text-accent-text"
									: "text-fg-muted hover:bg-surface hover:text-fg",
							)}
						>
							<Avatar email={auth.user.email} name={auth.user.display_name} size={38} />
							<div class="min-w-0 flex-1">
								<div class="truncate text-[14px] font-medium leading-tight text-fg">
									{auth.user.display_name || auth.user.email}
								</div>
								<div class="mt-0.5 truncate font-mono text-[11px] text-fg-faint">
									{roleLabel}{version ? ` · ${version}` : ""}
								</div>
							</div>
						</a>
						<button
							type="button"
							onclick={signOut}
							aria-label="Sign out"
							title="Sign out"
							class="grid h-11 w-11 shrink-0 place-items-center rounded-lg text-fg-muted transition-colors hover:bg-status-failed/10 hover:text-status-failed"
						>
							<LogOut size={19} aria-hidden="true" />
						</button>
					</div>
				{/if}
			</div>
		</div>
	</div>
{/if}
