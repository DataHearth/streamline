<script lang="ts">
	import Modal from "@components/modals/Modal.svelte";
	import Select from "@components/forms/Select.svelte";
	import ReleasesTable from "@components/shared/ReleasesTable.svelte";
	import { releaseLine, type Artist } from "@lib/music-books";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Manual search for an artist, as SeriesReleaseSearchModal is for a show:
	// the whole discography (packs) or one release. The hero opens it on the
	// discography and a release's button on that release; the scope switches
	// here without closing.
	let {
		open,
		artist,
		initialScope = "artist",
		onClose,
	}: {
		open: boolean;
		artist: Artist;
		// "artist" for the discography, or a release id as a string.
		initialScope?: string;
		onClose: () => void;
	} = $props();

	let scope = $state("artist");
	$effect(() => {
		if (open) scope = initialScope;
	});

	// Upcoming releases have nothing to find yet.
	let out = $derived(artist.albums.filter((r) => r.status !== "upcoming"));
	let options = $derived([
		{ value: "artist", label: i18n.music_whole_discography() },
		...out.map((r) => ({ value: String(r.id), label: `${r.title} · ${releaseLine(r)}` })),
	]);
	let release = $derived(out.find((r) => String(r.id) === scope));
	let base = $derived(release ? `/music/albums/${release.id}` : `/music/artists/${artist.id}`);
	// Tracks already on disk in the scope: above zero, a grab asks first.
	let onDisk = $derived(release ? release.tracks_have : out.reduce((n, r) => n + r.tracks_have, 0));
</script>

<Modal {open} title={i18n.manual_search_scope({ scope: artist.name })} size="4xl" {onClose}>
	<div class="mb-4 flex flex-wrap items-center gap-3">
		<span class="text-xs font-medium uppercase tracking-wide text-fg-subtle">{i18n.music_scope()}</span>
		<div class="w-64 max-w-full">
			<Select value={scope} {options} ariaLabel={i18n.music_search_scope()} onChange={(v) => (scope = v)} />
		</div>
	</div>
	{#key base}
		<ReleasesTable
			searchPath={release ? `${base}/search` : `${base}/browse`}
			grabPath={`${base}/grab`}
			queryKey={["releases", "music", base]}
			existingCount={onDisk}
			enabled={open}
			onGrabbed={onClose}
			media="music"
		/>
	{/key}
</Modal>
