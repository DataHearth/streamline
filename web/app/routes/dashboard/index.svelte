<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import { api } from "../../lib/api";
	import { posterUrl, tvPosterUrl } from "../../lib/posters";
	import { formatBytes } from "../../lib/format";
	import type {
		ActivityList,
		DiskUsage,
		DownloadQueue,
		MovieCounts,
		Movie,
		PaginatedMovies,
		PaginatedTVShows,
		SystemInfo,
		TVShow,
		TVShowCounts,
		UpcomingList as UpcomingResponse,
	} from "../../lib/types";
	import Hero, { type HeroItem } from "../../components/dashboard/Hero.svelte";
	import StatStrip from "../../components/dashboard/StatStrip.svelte";
	import RecentScroller from "../../components/dashboard/RecentScroller.svelte";
	import LiveQueuePanel from "../../components/dashboard/LiveQueuePanel.svelte";
	import RecentActivityPanel from "../../components/dashboard/RecentActivityPanel.svelte";
	import WantedScroller from "../../components/dashboard/WantedScroller.svelte";
	import UpcomingList from "../../components/shared/UpcomingList.svelte";
	import { upcomingEvents } from "../../lib/calendar";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	const moviesQuery = createQuery<PaginatedMovies>(() => ({
		queryKey: ["movies"],
		queryFn: () => api<PaginatedMovies>("/movies?page=1&limit=500"),
	}));

	const seriesQuery = createQuery<PaginatedTVShows>(() => ({
		queryKey: ["series"],
		queryFn: () => api<PaginatedTVShows>("/series?page=1&limit=500"),
	}));

	const countsQuery = createQuery<MovieCounts>(() => ({
		queryKey: ["movies", "counts"],
		queryFn: () => api<MovieCounts>("/movies/counts"),
	}));

	const seriesCountsQuery = createQuery<TVShowCounts>(() => ({
		queryKey: ["series", "counts"],
		queryFn: () => api<TVShowCounts>("/series/counts"),
	}));

	const activityQuery = createQuery<ActivityList>(() => ({
		queryKey: ["activity", "recent", 6],
		queryFn: () => api<ActivityList>("/activity?limit=6"),
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
	}));

	const systemQuery = createQuery<SystemInfo>(() => ({
		queryKey: ["system", "info"],
		queryFn: () => api<SystemInfo>("/system/info"),
	}));

	// The live queue. Activity polls this every 2s because it is the page you
	// watch; the dashboard is a glance, so it settles for 10.
	const queueQuery = createQuery<DownloadQueue>(() => ({
		queryKey: ["activity", "queue"],
		queryFn: () => api<DownloadQueue>("/activity/queue"),
		refetchInterval: 10000,
	}));
	let queue = $derived(queueQuery.data?.items ?? []);

	let allMovies = $derived(moviesQuery.data?.items ?? []);
	let allSeries = $derived(seriesQuery.data?.items ?? []);
	let monitoredMovies = $derived(allMovies.filter((m) => m.monitored).length);
	let monitoredSeries = $derived(allSeries.filter((s) => s.monitored).length);

	// Falls back to the data dir when neither library path can be probed, so the
	// tile still reports a real volume on a fresh install.
	let libraryDisks = $derived.by(() => {
		const sys = systemQuery.data;
		if (!sys) return [];
		const dirs = [
			{ label: i18n.lc_movies(), path: sys.library_dir, usage: sys.library_usage },
			{ label: "series", path: sys.series_dir, usage: sys.series_usage },
		].filter((d) => d.path && d.usage) as {
			label: string;
			path: string;
			usage: DiskUsage;
		}[];
		if (dirs.length > 0) return dirs;
		return sys.data_usage
			? [{ label: i18n.lc_data(), path: sys.data_dir, usage: sys.data_usage }]
			: [];
	});

	function movieToHero(m: Movie): HeroItem {
		const f = m.media_files?.[0];
		return {
			title: m.title,
			year: m.year,
			overview: m.overview,
			runtime: m.runtime,
			rating: m.rating,
			status: m.status,
			resolution: f?.parsed_resolution,
			codec: f?.parsed_codec,
			fileMeta: [formatBytes(f?.size, ""), f?.parsed_resolution, f?.parsed_source]
				.filter(Boolean)
				.join(" · "),
			posterSrc: posterUrl(m),
			href: `/movies/${m.id}`,
		};
	}
	function seriesToHero(s: TVShow): HeroItem {
		return {
			title: s.title,
			year: s.year,
			overview: s.overview,
			runtime: s.runtime,
			rating: s.rating,
			status: "available",
			posterSrc: tvPosterUrl(s.id),
			href: `/series/${s.id}`,
		};
	}

	// Feature an available title only (never a wanted one). Prefer a movie; fall
	// back to a fully-downloaded series. Keyed on files actually present, not on
	// a zero wanted count — unmonitoring a series zeroes that count too.
	let featuredMovie = $derived(allMovies.find((m) => m.status === "available"));
	let featuredSeries = $derived(
		allSeries.find(
			(s) =>
				(s.total_episodes ?? 0) > 0 &&
				(s.have_episodes ?? 0) >= (s.total_episodes ?? 0),
		),
	);
	let featured = $derived<HeroItem | undefined>(
		featuredMovie
			? movieToHero(featuredMovie)
			: featuredSeries
				? seriesToHero(featuredSeries)
				: undefined,
	);
	let recent = $derived(
		allMovies.filter((m) => m.status === "available").slice(0, 8),
	);
	let wanted = $derived(
		[
			// Unmonitored titles stay "wanted" server-side even though nothing is
			// searching for them, so the row would advertise work that never runs.
			...allMovies.filter((m) => m.status === "wanted" && m.monitored),
			...allSeries
				.filter((s) => s.monitored && (s.wanted_episodes ?? 0) > 0)
				.map((s) => ({
					id: s.id,
					title: s.title,
					year: s.year,
					status: "wanted" as const,
					href: `/series/${s.id}`,
					posterSrc: tvPosterUrl(s.id),
				})),
		].slice(0, 6),
	);
	let events = $derived(activityQuery.data?.events ?? []);
	let upcoming = $derived(upcomingEvents(upcomingQuery.data));
</script>

<div class="flex flex-col gap-5 pb-6 md:gap-6">
	<Hero
		item={featured}
		loading={moviesQuery.isLoading || seriesQuery.isLoading}
	/>

	<div
		class="mx-auto flex w-full max-w-7xl flex-col gap-9 px-4 md:px-8"
	>
		<StatStrip
			counts={countsQuery.data}
			seriesTotal={seriesCountsQuery.data?.total}
			{monitoredMovies}
			{monitoredSeries}
			{queue}
			disks={libraryDisks}
		/>

		<RecentScroller
			title={i18n.dash_recently_added()}
			movies={recent}
			emptyText={i18n.dash_no_movies_yet()}
		/>

		<section
			class="grid grid-cols-1 gap-4 lg:grid-cols-[1.4fr_1fr]"
			aria-label={i18n.nav_operations()}
		>
			<LiveQueuePanel {queue} />
			<RecentActivityPanel {events} />
		</section>

		<section
			class="grid grid-cols-1 gap-7 lg:grid-cols-[1fr_320px] lg:gap-8"
			aria-label={i18n.dash_wanted_upcoming()}
		>
			<WantedScroller movies={wanted} />
			<UpcomingList
				events={upcoming.slice(0, 4)}
				title={i18n.series_upcoming()}
				seeAllHref="/calendar"
				stretch
			/>
		</section>
	</div>
</div>
