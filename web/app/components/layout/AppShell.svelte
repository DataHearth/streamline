<script lang="ts">
	import { onMount, type Snippet } from "svelte";
	import Sidebar from "./Sidebar.svelte";
	import SidebarRail from "./SidebarRail.svelte";
	import BottomNav from "./BottomNav.svelte";
	import AddButton from "./AddButton.svelte";
	import TopBar from "./TopBar.svelte";
	import CommandPalette from "./CommandPalette.svelte";
	import SearchScreen from "./SearchScreen.svelte";
	import AddMovieModal from "../movies/AddMovieModal.svelte";
	import AddSeriesModal from "../series/AddSeriesModal.svelte";
	import MediaLookupScreen from "../shared/MediaLookupScreen.svelte";

	let { children }: { children: Snippet } = $props();

	let addMovieOpen = $state(false);
	let addSeriesOpen = $state(false);
	// Below md the add/request flow is a full-screen search rather than the split
	// modal, and search itself is a screen rather than the centred palette —
	// different components, not something CSS can pick.
	let compact = $state(false);

	onMount(() => {
		const onOpenMovie = () => (addMovieOpen = true);
		const onOpenSeries = () => (addSeriesOpen = true);
		window.addEventListener("streamline:open-add-movie", onOpenMovie);
		window.addEventListener("streamline:open-add-series", onOpenSeries);
		const mql = window.matchMedia("(max-width: 767px)");
		const syncCompact = () => (compact = mql.matches);
		syncCompact();
		mql.addEventListener("change", syncCompact);
		return () => {
			window.removeEventListener("streamline:open-add-movie", onOpenMovie);
			window.removeEventListener("streamline:open-add-series", onOpenSeries);
			mql.removeEventListener("change", syncCompact);
		};
	});
</script>

<div class="flex h-dvh overflow-hidden text-fg">
	<a
		href="#main"
		class="skip-link sr-only focus:not-sr-only rounded-md bg-accent px-3 py-2 text-sm font-semibold text-fg-on-accent shadow-lg"
	>
		Skip to main content
	</a>
	<Sidebar />
	<SidebarRail />
	<!-- scrollbar-gutter keeps the content width identical whether or not a route
	     scrolls; without it a taller tab reclaims the scrollbar's ~15px and
	     reflows anything sitting on a wrap boundary. -->
	<main
		id="main"
		class="min-w-0 flex-1 overflow-y-auto pb-16 [scrollbar-gutter:stable] md:pb-0"
	>
		<TopBar />
		{@render children?.()}
	</main>
	<BottomNav />
	<AddButton />
</div>
{#if compact}
	<SearchScreen />
{:else}
	<CommandPalette />
{/if}

{#if compact}
	<MediaLookupScreen
		kind="movie"
		open={addMovieOpen}
		onClose={() => (addMovieOpen = false)}
	/>
	<MediaLookupScreen
		kind="series"
		open={addSeriesOpen}
		onClose={() => (addSeriesOpen = false)}
	/>
{:else}
	<AddMovieModal open={addMovieOpen} onClose={() => (addMovieOpen = false)} />
	<AddSeriesModal open={addSeriesOpen} onClose={() => (addSeriesOpen = false)} />
{/if}

<style>
	/* Touch layouts have no pointer to grab a bar with, and a bar that appears
	   the moment content overflows steals width from the text under it — the
	   add sheet's synopsis reflowing mid-drag was the visible case. Hide the
	   bars below lg; scrolling itself is untouched. */
	@media (max-width: 1023px) {
		:global(*) {
			scrollbar-width: none;
			-ms-overflow-style: none;
		}
		:global(*::-webkit-scrollbar) {
			display: none;
		}
	}
</style>
