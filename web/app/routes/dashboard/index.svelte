<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import { api } from "../../lib/api";
	import { posterUrl, tvPosterUrl } from "../../lib/posters";
	import type {
		ActivityList,
		MovieCounts,
		Movie,
		MediaFile,
		PaginatedMovies,
		PaginatedTVShows,
		QueueItem,
		SystemInfo,
		TVShow,
		UpcomingList as UpcomingResponse,
	} from "../../lib/types";
	import Hero, { type HeroItem } from "../../components/dashboard/Hero.svelte";
	import StatStrip from "../../components/dashboard/StatStrip.svelte";
	import RecentScroller from "../../components/dashboard/RecentScroller.svelte";
	import LiveQueuePanel from "../../components/dashboard/LiveQueuePanel.svelte";
	import RecentActivityPanel from "../../components/dashboard/RecentActivityPanel.svelte";
	import WantedScroller from "../../components/dashboard/WantedScroller.svelte";
	import UpcomingList from "../../components/shared/UpcomingList.svelte";

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

	// /activity/queue is not yet exposed by the backend. Render the live queue
	// section's empty state until the endpoint lands.
	const queue: QueueItem[] = [];

	let allMovies = $derived(moviesQuery.data?.items ?? []);
	let allSeries = $derived(seriesQuery.data?.items ?? []);

	function pickPrimary(files?: MediaFile[]): MediaFile | undefined {
		if (!files || files.length === 0) return undefined;
		return [...files].sort((a, b) => b.size - a.size)[0];
	}
	function formatSize(bytes?: number): string {
		if (!bytes || bytes <= 0) return "";
		const gb = bytes / 1_073_741_824;
		if (gb >= 1) return `${gb.toFixed(1)} GB`;
		return `${(bytes / 1_048_576).toFixed(0)} MB`;
	}
	function movieToHero(m: Movie): HeroItem {
		const f = pickPrimary(m.media_files);
		return {
			title: m.title,
			year: m.year,
			overview: m.overview,
			runtime: m.runtime,
			rating: m.rating,
			status: m.status,
			resolution: f?.parsed_resolution,
			codec: f?.parsed_codec,
			fileMeta: [formatSize(f?.size), f?.parsed_resolution, f?.parsed_source]
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
	// back to a fully-downloaded series (has episodes, none still wanted).
	let featuredMovie = $derived(allMovies.find((m) => m.status === "available"));
	let featuredSeries = $derived(
		allSeries.find(
			(s) => (s.total_episodes ?? 0) > 0 && (s.wanted_episodes ?? 0) === 0,
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
	let wanted = $derived(allMovies.filter((m) => m.status === "wanted").slice(0, 6));
	let events = $derived(activityQuery.data?.events ?? []);
	let upcoming = $derived(upcomingQuery.data?.movies ?? []);
</script>

<div class="flex flex-col gap-9 pb-12">
	<Hero
		item={featured}
		loading={moviesQuery.isLoading || seriesQuery.isLoading}
	/>

	<div
		class="mx-auto flex w-full max-w-7xl flex-col gap-9 px-4 md:px-8"
	>
		<StatStrip
			counts={countsQuery.data}
			{queue}
			disk={systemQuery.data?.data_usage}
		/>

		<RecentScroller
			title="Recently added"
			movies={recent}
			seeAllHref="/movies"
			emptyText="No movies yet. Add one to see it here."
		/>

		<section
			class="grid grid-cols-1 gap-4 lg:grid-cols-[1.4fr_1fr]"
			aria-label="Operations"
		>
			<LiveQueuePanel {queue} />
			<RecentActivityPanel {events} />
		</section>

		<section
			class="grid grid-cols-1 gap-7 lg:grid-cols-[1fr_320px] lg:gap-8"
			aria-label="Wanted and upcoming"
		>
			<WantedScroller movies={wanted} />
			<UpcomingList events={upcoming} title="Upcoming" seeAllHref="/calendar" />
		</section>
	</div>
</div>
