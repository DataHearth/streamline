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
		ChevronRight,
		X,
	} from "@lucide/svelte";
	import { isActive as routifyIsActive } from "@roxi/routify";
	import { createQuery } from "@tanstack/svelte-query";
	import { api } from "../../lib/api";
	import type { PendingList, RequestCounts, SystemInfo } from "../../lib/types";
	import { auth } from "../../lib/auth.svelte";
	import { cn } from "../../lib/cn";
	import {
		TORRENT_PILLS,
		torrentCountsQuery,
		activityCurrent,
		type IsActiveFn,
	} from "../../lib/activity-nav";
	import { navCountsQuery, type NavDot } from "../../lib/nav-counts";
	import { bulkMode } from "../../lib/bulk-mode.svelte";
	import Avatar from "./Avatar.svelte";

	// Phone only. From md up the rail takes over (SidebarRail), and from lg the
	// full sidebar does.
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
	// The badge already says how many are pending, so the line carries the rest
	// of the picture rather than repeating it.
	let requestsLine = $derived.by(() => {
		const d = requestCountsQuery.data;
		if (!d) return "";
		if (!d.pending) return "Nothing waiting on you";
		return `${d.approved.toLocaleString()} approved · ${d.denied.toLocaleString()} denied`;
	});

	const pendingQuery = createQuery<PendingList>(() => ({
		queryKey: ["activity", "pending"],
		queryFn: () => api<PendingList>("/activity/pending"),
		enabled: auth.isAdmin,
		retry: false,
		refetchInterval: 30000,
	}));
	let pendingAdoptions = $derived(pendingQuery.data?.items.length ?? 0);

	const systemQuery = createQuery<SystemInfo>(() => ({
		queryKey: ["system", "info"],
		queryFn: () => api<SystemInfo>("/system/info"),
		retry: false,
	}));
	let version = $derived(systemQuery.data?.version ?? null);

	type Row = {
		label: string;
		href: string;
		icon: typeof Tv;
		line?: string;
		badge?: number;
		// Status counts in the line slot as coloured dots rather than plain text.
		dots?: NavDot[];
		// Torrents states go in the line slot as coloured dots, not as text.
		torrents?: boolean;
	};

	// Every fan-out in the bar raises the same sheet; only the rows differ.
	// Imports is an operation, not a library destination, so it sits under More
	// with Calendar and Requests — the same grouping the desktop sidebar uses.
	let libraryRows = $derived<Row[]>([
		{ label: "Movies", href: "/movies", icon: Film, line: counts.moviesLine },
		{ label: "Series", href: "/series", icon: Tv, line: counts.seriesLine },
	]);
	let activityRows = $derived<Row[]>([
		{
			label: "Queue & History",
			href: "/activity",
			icon: ListVideo,
			dots: counts.queueDots,
			line: counts.queueLine,
			badge: pendingAdoptions,
		},
		{
			label: "Torrents",
			href: "/activity/torrents",
			icon: Magnet,
			torrents: true,
		},
	]);
	let moreRows = $derived<Row[]>([
		{ label: "Calendar", href: "/calendar", icon: CalendarDays },
		{
			label: "Requests",
			href: "/requests",
			icon: Inbox,
			line: requestsLine,
			badge: pendingRequests,
		},
		...(auth.isAdmin
			? [
					{
						label: "Imports",
						href: "/library/imports",
						icon: FolderInput,
						line: counts.importsLine,
					},
				]
			: []),
	]);
	// Settings sits below a hairline rather than under an "Admin" heading — one
	// rule says the same thing in less space.
	let adminRows = $derived<Row[]>(
		auth.isAdmin
			? [{ label: "Settings", href: "/settings", icon: Settings }]
			: [],
	);

	const SECTIONS = ["Library", "Activity", "More"] as const;
	type Section = (typeof SECTIONS)[number];

	let sheet = $state<Section | "">("");
	let sheetRows = $derived<Row[]>(
		sheet === "Library" ? libraryRows : sheet === "Activity" ? activityRows : moreRows,
	);

	// Secondary routes light up the cell that reaches them, so the bar still
	// says where you are while you are on a sheet-reached page.
	const IN_MORE = [
		"/calendar",
		"/requests",
		"/library/imports",
		"/settings",
		"/account",
	];
	let libraryActive = $derived(["/movies", "/series"].some((p) => isActiveFn(p)));
	let activityActive = $derived(isActiveFn("/activity"));
	let moreActive = $derived(IN_MORE.some((p) => isActiveFn(p)));
	let moreOn = $derived(moreActive || sheet === "More");

	const primary = [
		{ label: "Dashboard", href: "/dashboard", icon: LayoutDashboard },
		{ label: "Library", icon: Library, section: "Library" as const },
		{ label: "Activity", icon: Activity, section: "Activity" as const },
	];
	function cellActive(label: string) {
		if (label === "Library") return libraryActive;
		if (label === "Activity") return activityActive;
		return false;
	}

	function closeSheet() {
		sheet = "";
	}
	function toggleSheet(s: Section) {
		sheet = sheet === s ? "" : s;
	}

	// Routify intercepts internal <a> clicks and navigates client-side; close
	// the sheet on any click inside it so a tapped link doesn't leave the
	// overlay hanging over the new page.
	function onSheetClick(e: MouseEvent) {
		if ((e.target as HTMLElement).closest("a")) closeSheet();
	}

	$effect(() => {
		if (!sheet) return;
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

	// Drag the sheet down to dismiss. Pointer events so a mouse works too; the
	// drag only starts from the header or from a list already scrolled to the
	// top, otherwise the gesture belongs to the list. Past a quarter of the
	// sheet's height — or on a flick — it closes; anything less springs back.
	const DISMISS_RATIO = 0.25;
	const FLICK = 0.5; // px per ms
	function swipeSheet(node: HTMLElement) {
		let id: number | null = null;
		let startY = 0;
		let startedAt = 0;
		let dy = 0;
		let dragging = false;
		const backdrop = () =>
			node.parentElement?.querySelector<HTMLElement>('[data-sheet-backdrop=""]');

		const paint = () => {
			node.style.transform = dy > 0 ? `translate3d(0, ${dy}px, 0)` : "";
			const b = backdrop();
			if (b) b.style.opacity = String(Math.max(0.25, 1 - dy / (node.offsetHeight || 1)));
		};
		const reset = (animate: boolean) => {
			node.style.transition = animate
				? "transform var(--dur-base, 200ms) var(--ease, ease-out)"
				: "";
			node.style.transform = "";
			const b = backdrop();
			if (b) b.style.opacity = "";
		};

		const onDown = (e: PointerEvent) => {
			if (id !== null || e.button !== 0) return;
			const target = e.target as HTMLElement;
			const scroller = node.querySelector<HTMLElement>("[data-sheet-scroll]");
			if (scroller?.contains(target) && scroller.scrollTop > 0) return;
			id = e.pointerId;
			startY = e.clientY;
			startedAt = e.timeStamp;
			dy = 0;
		};
		const onMove = (e: PointerEvent) => {
			if (e.pointerId !== id) return;
			const delta = e.clientY - startY;
			// 6px of slop so a tap on a row is still a tap.
			if (!dragging) {
				if (delta < 6) return;
				dragging = true;
				node.style.transition = "none";
				// Capture keeps the gesture alive if the finger leaves the sheet;
				// it can legitimately fail (pointer already released), and the
				// drag still works without it.
				try {
					node.setPointerCapture(e.pointerId);
				} catch {}
			}
			// Resistance above the resting position rather than a gap under it.
			dy = delta > 0 ? delta : delta / 4;
			paint();
		};
		const onUp = (e: PointerEvent) => {
			if (e.pointerId !== id) return;
			const velocity = dy / Math.max(1, e.timeStamp - startedAt);
			const far = dy > (node.offsetHeight || 0) * DISMISS_RATIO;
			id = null;
			if (!dragging) return;
			dragging = false;
			if (far || velocity > FLICK) {
				reset(false); // hand the exit to the fly transition
				closeSheet();
			} else {
				reset(true);
			}
		};
		const onCancel = (e: PointerEvent) => {
			if (e.pointerId !== id) return;
			id = null;
			dragging = false;
			reset(true);
		};

		node.addEventListener("pointerdown", onDown);
		node.addEventListener("pointermove", onMove);
		node.addEventListener("pointerup", onUp);
		node.addEventListener("pointercancel", onCancel);
		return {
			destroy() {
				node.removeEventListener("pointerdown", onDown);
				node.removeEventListener("pointermove", onMove);
				node.removeEventListener("pointerup", onUp);
				node.removeEventListener("pointercancel", onCancel);
			},
		};
	}

	let roleLabel = $derived.by(() => {
		const r = auth.user?.role;
		if (r === "admin") return "admin";
		if (r === "request_only") return "request";
		return "member";
	});

	const cellBase =
		"relative flex flex-col items-center justify-center gap-1 px-2 pt-2.5 pb-3 text-[10.5px] transition-colors";
	const cellOn =
		"text-accent-text before:absolute before:inset-x-[18%] before:top-0 before:h-0.5 before:rounded-b-sm before:bg-accent";
	const cellOff = "text-fg-subtle hover:text-fg-muted";
	const rowBase =
		"relative flex items-center gap-3.5 rounded-xl px-2.5 py-3 transition-colors";
	const rowInactive = "text-fg-muted hover:bg-surface hover:text-fg";
	const rowActive = "bg-accent-soft text-accent-text";
</script>

<nav
	class={cn(
		"fixed inset-x-0 bottom-0 z-40 grid grid-cols-4 min-h-14 border-t border-border bg-bg-elevated/95 pb-[env(safe-area-inset-bottom)] backdrop-blur-md saturate-150 md:hidden",
		// A phone bulk selection puts its action bar here rather than stacking a
		// second bar on top of this one.
		bulkMode.active && "hidden",
	)}
	aria-label="Primary"
>
	{#each primary as tab (tab.label)}
		{#if tab.section}
			{@const on = cellActive(tab.label) || sheet === tab.section}
			<button
				type="button"
				onclick={() => toggleSheet(tab.section)}
				aria-haspopup="dialog"
				aria-expanded={sheet === tab.section}
				aria-label={tab.label}
				class={cn(cellBase, on ? cellOn : cellOff)}
			>
				<tab.icon size={20} strokeWidth={on ? 2 : 1.6} />
				<span>{tab.label}</span>
			</button>
		{:else}
			{@const active = isActiveFn(tab.href ?? "")}
			<a
				href={tab.href}
				aria-current={active ? "page" : undefined}
				class={cn(cellBase, active ? cellOn : cellOff)}
			>
				<tab.icon size={20} strokeWidth={active ? 2 : 1.6} />
				<span>{tab.label}</span>
			</a>
		{/if}
	{/each}

	<button
		type="button"
		onclick={() => toggleSheet("More")}
		aria-haspopup="dialog"
		aria-expanded={sheet === "More"}
		aria-label="More destinations"
		class={cn(cellBase, moreOn ? cellOn : cellOff)}
	>
		<div class="relative">
			<MoreHorizontal size={20} strokeWidth={moreOn ? 2 : 1.6} />
			{#if pendingRequests > 0 && sheet !== "More"}
				<span
					class="absolute -right-1.5 -top-1 h-1.5 w-1.5 rounded-full bg-status-wanted"
					aria-hidden="true"
				></span>
			{/if}
		</div>
		<span>More</span>
	</button>
</nav>

{#if sheet}
	<div
		class="fixed inset-0 z-[60] md:hidden"
		role="dialog"
		aria-modal="true"
		aria-label={sheet}
	>
		<button
			type="button"
			aria-label="Close menu"
			data-sheet-backdrop=""
			transition:fade={{ duration: 180 }}
			onclick={closeSheet}
			class="absolute inset-0 h-full w-full cursor-default bg-black/55 backdrop-blur-[2px]"
		></button>

		<div
			use:swipeSheet
			transition:fly={{ y: 420, duration: 300, easing: cubicOut }}
			onclick={onSheetClick}
			class="absolute inset-x-0 bottom-0 flex max-h-[85dvh] flex-col overflow-hidden rounded-t-2xl border-t border-border-strong bg-bg-elevated shadow-4"
		>
			<div
				class="relative flex cursor-grab touch-none select-none items-center justify-between px-5 pb-1 pt-4 active:cursor-grabbing"
			>
				<span
					aria-hidden="true"
					class="absolute left-1/2 top-2 h-1 w-9 -translate-x-1/2 rounded-full bg-border-strong"
				></span>
				<h2 class="text-[17px] font-semibold tracking-tight text-fg">{sheet}</h2>
				<button
					type="button"
					onclick={closeSheet}
					aria-label="Close"
					class="grid h-9 w-9 place-items-center rounded-full bg-surface text-fg-subtle transition hover:bg-bg-hover hover:text-fg"
				>
					<X size={16} aria-hidden="true" />
				</button>
			</div>

			<div
				data-sheet-scroll
				class="min-h-0 flex-1 overscroll-contain overflow-y-auto px-2.5 pb-[max(env(safe-area-inset-bottom),12px)] pt-1"
			>
				<ul class="flex flex-col gap-0.5">
					{#each sheetRows as row (row.href)}
						{@const active =
							sheet === "Activity"
								? activityCurrent(isActiveFn, row.href)
								: isActiveFn(row.href)}
						<li>
							<a
								href={row.href}
								aria-current={active ? "page" : undefined}
								class={cn(rowBase, active ? rowActive : rowInactive)}
							>
								<row.icon size={22} class="shrink-0" />
								<span class="min-w-0 flex-1">
									<span class="block text-[15px] font-medium leading-tight tracking-tight">
										{row.label}
									</span>
									{#if row.torrents}
										<span
											class="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 font-mono text-[11px] leading-none tabular-nums text-fg-subtle"
										>
											{#each TORRENT_PILLS as p (p.key)}
												{#if (torrentCounts.counts[p.key] ?? 0) > 0}
													<span class="flex items-center gap-1.5">
														<span
															class="h-[6px] w-[6px] rounded-full"
															style:background-color="var(--status-{p.dot})"
														></span>
														{torrentCounts.counts[p.key]}
														{p.key}
													</span>
												{/if}
											{/each}
										</span>
									{:else if row.dots?.length}
										<span
											class="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 font-mono text-[11px] leading-none tabular-nums text-fg-subtle"
										>
											{#each row.dots as p (p.key)}
												<span class="flex items-center gap-1.5">
													<span
														class="h-[6px] w-[6px] rounded-full"
														style:background-color="var(--status-{p.dot})"
													></span>
													{p.count}
													{p.label}
												</span>
											{/each}
										</span>
									{:else if row.line}
										<span
											class={cn(
												"mt-1 block truncate font-mono text-[11px]",
												active ? "text-accent-text opacity-80" : "text-fg-subtle",
											)}
										>
											{row.line}
										</span>
									{/if}
								</span>
								{#if row.badge}
									<span
										class="shrink-0 rounded-full bg-status-wanted/20 px-2 py-0.5 font-mono text-[11px] tabular-nums text-status-wanted"
									>
										{row.badge.toLocaleString()}
									</span>
								{:else}
									<ChevronRight
										size={18}
										class={cn("shrink-0", active ? "text-accent-text" : "text-fg-faint")}
										aria-hidden="true"
									/>
								{/if}
							</a>
						</li>
					{/each}
				</ul>

				{#if sheet === "More" && adminRows.length}
					<div class="my-2 h-px bg-border" role="presentation"></div>
					<ul class="flex flex-col gap-0.5">
						{#each adminRows as row (row.href)}
							{@const active = isActiveFn(row.href)}
							<li>
								<a
									href={row.href}
									aria-current={active ? "page" : undefined}
									class={cn(rowBase, active ? rowActive : rowInactive)}
								>
									<row.icon size={22} class="shrink-0" />
									<span class="min-w-0 flex-1 text-[15px] font-medium tracking-tight">
										{row.label}
									</span>
									<ChevronRight
										size={18}
										class={cn("shrink-0", active ? "text-accent-text" : "text-fg-faint")}
										aria-hidden="true"
									/>
								</a>
							</li>
						{/each}
					</ul>
				{/if}

				{#if sheet === "More" && auth.user}
					<div class="mt-2 flex items-center gap-2 border-t border-border px-1 pt-3">
						<a
							href="/account"
							aria-current={isActiveFn("/account") ? "page" : undefined}
							class={cn(
								"flex min-w-0 flex-1 items-center gap-3 rounded-lg px-1.5 py-2 transition-colors",
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
