<script lang="ts">
	import { onMount, type Snippet } from "svelte";
	import Sidebar from "./Sidebar.svelte";
	import BottomNav from "./BottomNav.svelte";
	import TopBar from "./TopBar.svelte";
	import CommandPalette from "./CommandPalette.svelte";
	import AddMovieModal from "../movies/AddMovieModal.svelte";
	import AddSeriesModal from "../series/AddSeriesModal.svelte";

	let { children }: { children: Snippet } = $props();

	let addMovieOpen = $state(false);
	let addSeriesOpen = $state(false);

	onMount(() => {
		const onOpenMovie = () => (addMovieOpen = true);
		const onOpenSeries = () => (addSeriesOpen = true);
		window.addEventListener("streamline:open-add-movie", onOpenMovie);
		window.addEventListener("streamline:open-add-series", onOpenSeries);
		return () => {
			window.removeEventListener("streamline:open-add-movie", onOpenMovie);
			window.removeEventListener("streamline:open-add-series", onOpenSeries);
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
	<main id="main" class="min-w-0 flex-1 overflow-y-auto pb-16 lg:pb-0">
		<TopBar />
		{@render children?.()}
	</main>
	<BottomNav />
</div>
<CommandPalette />
<AddMovieModal open={addMovieOpen} onClose={() => (addMovieOpen = false)} />
<AddSeriesModal open={addSeriesOpen} onClose={() => (addSeriesOpen = false)} />
