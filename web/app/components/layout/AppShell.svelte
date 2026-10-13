<script lang="ts">
	import { onMount, type Snippet } from "svelte";
	import Sidebar from "./Sidebar.svelte";
	import SidebarRail from "./SidebarRail.svelte";
	import BottomNav from "./BottomNav.svelte";
	import AddButton from "./AddButton.svelte";
	import TopBar from "./TopBar.svelte";
	import CommandPalette from "./CommandPalette.svelte";
	import SearchScreen from "./SearchScreen.svelte";
	import AddMovieModal from "@components/movies/AddMovieModal.svelte";
	import AddSeriesModal from "@components/series/AddSeriesModal.svelte";
	import MediaLookupScreen from "@components/shared/MediaLookupScreen.svelte";
	import AddMusicBookModal from "@components/shared/AddMusicBookModal.svelte";
	import MusicBookLookupScreen from "@components/shared/MusicBookLookupScreen.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	let { children }: { children: Snippet } = $props();

	let addMovieOpen = $state(false);
	let addSeriesOpen = $state(false);
	let addArtistOpen = $state(false);
	let addBookOpen = $state(false);
	// Below md the add/request flow is a full-screen search rather than the split
	// modal — different components, not something CSS can pick.
	let compact = $state(false);
	// Phones and tablets get the full-screen search, whose field and results sit
	// above an on-screen keyboard; desktops (mouse or trackpad) get the centred
	// command palette. A device counts as touch when touch is its primary input,
	// so a tablet with a mouse attached keeps the full-screen search; any window
	// below md gets it too. Width alone could not decide: a landscape tablet
	// (≈1180px) is as wide as a laptop, and its keyboard covered most of the
	// palette.
	let touchSearch = $state(false);

	onMount(() => {
		const onOpenMovie = () => (addMovieOpen = true);
		const onOpenSeries = () => (addSeriesOpen = true);
		const onOpenArtist = () => (addArtistOpen = true);
		const onOpenBook = () => (addBookOpen = true);
		window.addEventListener("streamline:open-add-movie", onOpenMovie);
		window.addEventListener("streamline:open-add-series", onOpenSeries);
		window.addEventListener("streamline:open-add-artist", onOpenArtist);
		window.addEventListener("streamline:open-add-book", onOpenBook);
		const mql = window.matchMedia("(max-width: 767px)");
		const syncCompact = () => (compact = mql.matches);
		syncCompact();
		mql.addEventListener("change", syncCompact);
		const touch = window.matchMedia("(max-width: 767px), (hover: none) and (pointer: coarse)");
		const syncTouch = () => (touchSearch = touch.matches);
		syncTouch();
		touch.addEventListener("change", syncTouch);
		return () => {
			window.removeEventListener("streamline:open-add-movie", onOpenMovie);
			window.removeEventListener("streamline:open-add-series", onOpenSeries);
			window.removeEventListener("streamline:open-add-artist", onOpenArtist);
			window.removeEventListener("streamline:open-add-book", onOpenBook);
			mql.removeEventListener("change", syncCompact);
			touch.removeEventListener("change", syncTouch);
		};
	});
</script>

<div class="flex h-dvh overflow-hidden text-fg">
	<a
		href="#main"
		class="skip-link sr-only focus:not-sr-only rounded-md bg-accent px-3 py-2 text-sm font-semibold text-fg-on-accent shadow-lg"
	>
		{i18n.a11y_skip_to_content()}
	</a>
	<Sidebar />
	<SidebarRail />
	<!-- scrollbar-gutter keeps the content width identical whether or not a route
	     scrolls; without it a taller tab reclaims the scrollbar's ~15px and
	     reflows anything sitting on a wrap boundary. `relative` makes it the
	     containing block of every absolute descendant with no positioned
	     ancestor of its own (an `sr-only` label, mostly): without it those
	     resolve against <body>, escape this scroller's clip and stretch the
	     document, which then draws a second scrollbar beside this one. -->
	<main
		id="main"
		class="relative min-w-0 flex-1 overflow-y-auto pb-20 [scrollbar-gutter:stable] md:pb-0"
	>
		<TopBar />
		{@render children?.()}
	</main>
	<BottomNav />
	<AddButton />
</div>
{#if touchSearch}
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
	<MusicBookLookupScreen kind="artist" open={addArtistOpen} onClose={() => (addArtistOpen = false)} />
	<MusicBookLookupScreen kind="book" open={addBookOpen} onClose={() => (addBookOpen = false)} />
{:else}
	<AddMovieModal open={addMovieOpen} onClose={() => (addMovieOpen = false)} />
	<AddSeriesModal open={addSeriesOpen} onClose={() => (addSeriesOpen = false)} />
	<AddMusicBookModal kind="artist" open={addArtistOpen} onClose={() => (addArtistOpen = false)} />
	<AddMusicBookModal kind="book" open={addBookOpen} onClose={() => (addBookOpen = false)} />
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
