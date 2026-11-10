<script lang="ts">
	import { onMount, type Snippet } from "svelte";
	import Sidebar from "./Sidebar.svelte";
	import SidebarRail from "./SidebarRail.svelte";
	import BottomNav from "./BottomNav.svelte";
	import AddButton from "./AddButton.svelte";
	import TopBar from "./TopBar.svelte";
	import CommandPalette from "./CommandPalette.svelte";
	import AddMovieModal from "@components/movies/AddMovieModal.svelte";
	import AddSeriesModal from "@components/series/AddSeriesModal.svelte";
	import MediaLookupScreen from "@components/shared/MediaLookupScreen.svelte";
	import AddMusicBookModal from "@components/shared/AddMusicBookModal.svelte";
	import MusicBookLookupScreen from "@components/shared/MusicBookLookupScreen.svelte";
	import { restoreScrollOnBack } from "@lib/scroll-restore";
	import { BELOW_MD } from "@lib/viewport.svelte";
	import { onOpenAdd } from "@lib/add-flow";
	import { m as i18n } from "@lib/paraglide/messages.js";

	let { children }: { children: Snippet } = $props();

	let addMovieOpen = $state(false);
	let addSeriesOpen = $state(false);
	let addArtistOpen = $state(false);
	let addBookOpen = $state(false);
	// Set on every open, so an opener sending no query clears the last one.
	let addQuery = $state("");
	// Below md the add/request flow is a full-screen search rather than the split
	// modal, and search itself is a page (/search) rather than the centred
	// palette — different components, not something CSS can pick. From md the
	// palette is mounted for a mouse's search bar and for Ctrl/⌘+K on any
	// pointer; a touch screen's search bar is a link to /search (TopBar).
	let compact = $state(false);
	let main = $state<HTMLElement | null>(null);

	onMount(() => (main ? restoreScrollOnBack(main) : undefined));

	onMount(() => {
		const offAdd = onOpenAdd((kind, query) => {
			addQuery = query;
			if (kind === "movie") addMovieOpen = true;
			else if (kind === "series") addSeriesOpen = true;
			else if (kind === "artist") addArtistOpen = true;
			else addBookOpen = true;
		});
		const mql = window.matchMedia(BELOW_MD);
		const syncCompact = () => (compact = mql.matches);
		syncCompact();
		mql.addEventListener("change", syncCompact);
		return () => {
			offAdd();
			mql.removeEventListener("change", syncCompact);
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
	     document, which then draws a second scrollbar beside this one.
	     The bottom padding clears the fixed BottomNav below md, which is its
	     row plus the safe-area inset, so the inset belongs here as well: on a
	     device with one the nav otherwise covers the end of every page. -->
	<main
		bind:this={main}
		id="main"
		class="relative min-w-0 flex-1 overflow-y-auto pb-[calc(5rem+env(safe-area-inset-bottom))] [scrollbar-gutter:stable] md:pb-0"
	>
		<TopBar />
		{@render children?.()}
	</main>
	<BottomNav />
	<AddButton />
</div>
{#if !compact}
	<CommandPalette />
{/if}

{#if compact}
	<MediaLookupScreen
		kind="movie"
		open={addMovieOpen}
		seedQuery={addQuery}
		onClose={() => (addMovieOpen = false)}
	/>
	<MediaLookupScreen
		kind="series"
		open={addSeriesOpen}
		seedQuery={addQuery}
		onClose={() => (addSeriesOpen = false)}
	/>
	<MusicBookLookupScreen
		kind="artist"
		open={addArtistOpen}
		seedQuery={addQuery}
		onClose={() => (addArtistOpen = false)}
	/>
	<MusicBookLookupScreen
		kind="book"
		open={addBookOpen}
		seedQuery={addQuery}
		onClose={() => (addBookOpen = false)}
	/>
{:else}
	<AddMovieModal open={addMovieOpen} seedQuery={addQuery} onClose={() => (addMovieOpen = false)} />
	<AddSeriesModal open={addSeriesOpen} seedQuery={addQuery} onClose={() => (addSeriesOpen = false)} />
	<AddMusicBookModal
		kind="artist"
		open={addArtistOpen}
		seedQuery={addQuery}
		onClose={() => (addArtistOpen = false)}
	/>
	<AddMusicBookModal
		kind="book"
		open={addBookOpen}
		seedQuery={addQuery}
		onClose={() => (addBookOpen = false)}
	/>
{/if}

<style>
	/* Touch layouts have no pointer to grab a bar with, and a bar that appears
	   the moment content overflows steals width from the text under it — the
	   add sheet's synopsis reflowing mid-drag is the visible case. Hide the
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
