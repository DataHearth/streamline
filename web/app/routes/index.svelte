<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import { api, apiAllPages, type Paginated } from "@lib/api";
	import { SILENT } from "@lib/query";
	import { auth } from "@lib/auth.svelte";
	import type {
		DownloadQueue,
		ImportScanList,
		LibraryArrivals,
		MediaRequest,
		Movie,
		SystemInfo,
		TVShow,
		UpcomingList as UpcomingResponse,
	} from "@lib/types";
	import type { Artist, ShelfItem } from "@lib/music-books";
	import ArrivalsShelf from "@components/dashboard/ArrivalsShelf.svelte";
	import NeedsYou, { type NeedItem } from "@components/dashboard/NeedsYou.svelte";
	import MyRequests from "@components/dashboard/MyRequests.svelte";
	import LibraryLedger from "@components/dashboard/LibraryLedger.svelte";
	import LiveQueuePanel from "@components/dashboard/LiveQueuePanel.svelte";
	import UpcomingList from "@components/shared/UpcomingList.svelte";
	import { upcomingEvents } from "@lib/calendar";
	import { booksRow, freeSpace, movieRow, musicRow, seriesRow, type LedgerRow } from "@lib/dashboard";
	import { entryHeading } from "@lib/activity-touch";
	import { KIND_CHIPS, KIND_MEDIA } from "@lib/requests-touch";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Nothing on the dashboard raises the global loading bar (`meta.silent`):
	// it is a glance, every panel has its own skeleton, and the queue below
	// polls on a timer nobody asked for.

	// What landed, across the four media. Polled slowly: an import is not an
	// event anyone waits on from here.
	const arrivalsQuery = createQuery<LibraryArrivals>(() => ({
		queryKey: ["library", "recent"],
		queryFn: () => api<LibraryArrivals>("/library/recent?limit=24"),
		refetchInterval: 60000,
		meta: SILENT,
	}));

	// The ledger counts each library in its own unit, which the counts
	// endpoints do not all carry (episodes have/total, releases out), so it
	// reads the lists. Their keys sit beside the library pages' own and never
	// collide with them; each prefix is still what an add or a change
	// invalidates.
	const moviesQuery = createQuery<Paginated<Movie>>(() => ({
		queryKey: ["movies"],
		queryFn: () => apiAllPages<Movie>("/movies"),
		meta: SILENT,
	}));
	const seriesQuery = createQuery<Paginated<TVShow>>(() => ({
		queryKey: ["series"],
		queryFn: () => apiAllPages<TVShow>("/series"),
		meta: SILENT,
	}));
	const artistsQuery = createQuery<Paginated<Artist>>(() => ({
		queryKey: ["music", "artists", "all"],
		queryFn: () => apiAllPages<Artist>("/music/artists"),
		meta: SILENT,
	}));
	const shelfQuery = createQuery<Paginated<ShelfItem>>(() => ({
		queryKey: ["books", "list", "all"],
		queryFn: () => apiAllPages<ShelfItem>("/books"),
		meta: SILENT,
	}));

	// A reviewer reads the pending ones off it; a request_only member gets
	// only their own, which is what their panel lists.
	const requestsQuery = createQuery<Paginated<MediaRequest>>(() => ({
		queryKey: ["requests", "dashboard"],
		queryFn: () => api<Paginated<MediaRequest>>("/requests?limit=100"),
		enabled: !!auth.user,
		meta: SILENT,
	}));

	// No status filter on the wire: imports have no counts endpoint and the
	// newest hundred always hold every scan still awaiting review (see
	// lib/nav-counts).
	const importsQuery = createQuery<ImportScanList>(() => ({
		queryKey: ["imports", "dashboard"],
		queryFn: () => api<ImportScanList>("/library/imports?limit=100"),
		enabled: auth.isAdmin,
		meta: SILENT,
	}));

	function upcomingRange() {
		const now = new Date();
		const to = new Date(now.getTime() + 30 * 86_400_000);
		return { from: now.toISOString(), to: to.toISOString() };
	}
	const upcomingQuery = createQuery<UpcomingResponse>(() => ({
		queryKey: ["calendar", "upcoming", 30],
		queryFn: () => {
			const { from, to } = upcomingRange();
			return api<UpcomingResponse>(
				`/calendar/upcoming?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`,
			);
		},
		meta: SILENT,
	}));

	const systemQuery = createQuery<SystemInfo>(() => ({
		queryKey: ["system", "info"],
		queryFn: () => api<SystemInfo>("/system/info"),
		enabled: auth.isAdmin,
		meta: SILENT,
	}));

	// The live queue. Activity polls this every 2s because it is the page you
	// watch; the dashboard is a glance, so it settles for 10.
	const queueQuery = createQuery<DownloadQueue>(() => ({
		queryKey: ["activity", "queue"],
		queryFn: () => api<DownloadQueue>("/activity/queue"),
		refetchInterval: 10000,
		meta: SILENT,
	}));

	let queue = $derived(queueQuery.data?.items ?? []);
	let allMovies = $derived(moviesQuery.data?.items ?? []);
	let requests = $derived(requestsQuery.data?.items ?? []);
	let arrivals = $derived(arrivalsQuery.data?.items ?? []);
	let upcoming = $derived(upcomingEvents(upcomingQuery.data));

	// A medium whose list failed drops out rather than reading as empty.
	let ledger = $derived<LedgerRow[]>(
		[
			moviesQuery.data && movieRow(moviesQuery.data.items),
			seriesQuery.data && seriesRow(seriesQuery.data.items),
			artistsQuery.data && musicRow(artistsQuery.data.items),
			shelfQuery.data && booksRow(shelfQuery.data.items),
		].filter((r): r is LedgerRow => !!r),
	);
	let ledgerLoading = $derived(
		moviesQuery.isLoading || seriesQuery.isLoading || artistsQuery.isLoading || shelfQuery.isLoading,
	);

	const word = (count: number, one: () => string, other: () => string) => (count === 1 ? one : other)();

	// What waits on the person looking, in the order it blocks something: a
	// held download is stuck until someone decides, then the people waiting on
	// a request, then a scan nobody has reviewed, then grabs that gave up.
	// Held downloads and imports are an admin's to resolve; any reviewer
	// answers requests. Absent when it is zero, so a quiet day shows nothing.
	let needs = $derived.by<NeedItem[]>(() => {
		if (!auth.canAddDirectly) return [];
		const out: NeedItem[] = [];
		if (auth.isAdmin) {
			const held = queue.filter((q) => q.status === "held");
			if (held.length)
				out.push({
					key: "held",
					count: held.length,
					label: word(held.length, i18n.dash_needs_held_one, i18n.dash_needs_held_other),
					detail: held.map(entryHeading).join(" · "),
					href: "/activity",
					token: "held",
				});
		}
		const pending = requests.filter((r) => r.status === "pending");
		if (pending.length)
			out.push({
				key: "requests",
				count: pending.length,
				label: word(pending.length, i18n.dash_needs_requests_one, i18n.dash_needs_requests_other),
				detail: KIND_CHIPS.flatMap(({ key, label }) => {
					if (key === "all") return [];
					const n = pending.filter((r) => KIND_MEDIA[key].includes(r.media_type)).length;
					return n ? [`${label} ${n}`] : [];
				}).join(" · "),
				href: "/requests",
				token: "wanted",
			});
		if (auth.isAdmin) {
			const scans = (importsQuery.data?.items ?? []).filter((s) => s.status === "awaiting_review");
			if (scans.length)
				out.push({
					key: "imports",
					count: scans.length,
					label: word(scans.length, i18n.dash_needs_imports_one, i18n.dash_needs_imports_other),
					detail: scans.map((s) => s.source_path).join(" · "),
					href: scans.length === 1 && scans[0] ? `/imports/${scans[0].id}` : "/imports",
					token: "wanted",
				});
		}
		const failed = allMovies.filter((m) => m.status === "failed");
		if (failed.length)
			out.push({
				key: "failed",
				count: failed.length,
				label: word(failed.length, i18n.dash_needs_failed_one, i18n.dash_needs_failed_other),
				detail: failed.map((m) => m.title).join(" · "),
				href: "/movies?status=failed",
				token: "failed",
			});
		return out;
	});

	let isRequester = $derived(!!auth.user && !auth.canAddDirectly);
	let hasTop = $derived(isRequester ? requests.length > 0 : needs.length > 0);
</script>

{#snippet top()}
	{#if isRequester}
		<MyRequests {requests} />
	{:else}
		<NeedsYou items={needs} />
	{/if}
{/snippet}

<div class="flex flex-col gap-8 pb-8 md:gap-10 md:pb-12">
	<ArrivalsShelf
		{arrivals}
		loading={arrivalsQuery.isLoading}
		error={arrivalsQuery.isError}
		top={hasTop ? top : undefined}
	/>

	<div class="mx-auto w-full max-w-7xl px-4 md:px-8">
		<LibraryLedger
			rows={ledger}
			loading={ledgerLoading && ledger.length === 0}
			free={auth.isAdmin ? freeSpace(systemQuery.data) || undefined : undefined}
		/>
	</div>

	<div class="mx-auto w-full max-w-7xl px-4 md:px-8">
		{#if isRequester}
			<UpcomingList
				events={upcoming.slice(0, 5)}
				title={i18n.series_upcoming()}
				seeAllHref="/calendar"
			/>
		{:else}
			<section
				class="grid grid-cols-1 gap-6 lg:grid-cols-[1.4fr_1fr] lg:gap-4"
				aria-label={i18n.nav_operations()}
			>
				<LiveQueuePanel {queue} />
				<UpcomingList
					events={upcoming.slice(0, 4)}
					title={i18n.series_upcoming()}
					seeAllHref="/calendar"
					stretch
				/>
			</section>
		{/if}
	</div>
</div>
