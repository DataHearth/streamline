<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import { api } from "@lib/api";
	import type { DiscographyFiles, ReleaseFiles } from "@lib/music-books";
	import type { PackFiles, SearchResult } from "@lib/types";
	import ReleaseFilesView from "./ReleaseFilesView.svelte";

	// What a release's torrent holds, read against the scope before anything is
	// grabbed: an album's tracks, a season's or show's episodes, or an artist's
	// waiting albums for a discography pack. One request
	// to the indexer per release, so it only runs once someone asks, and a
	// failure is not retried behind their back.
	let {
		path,
		release,
		kind,
	}: { path: string; release: SearchResult; kind: "tracks" | "episodes" | "discography" } = $props();

	const q = createQuery(() => ({
		queryKey: ["release-files", path, release.download_url],
		queryFn: () => api<ReleaseFiles | PackFiles | DiscographyFiles>(path, { method: "POST", body: release }),
		staleTime: 5 * 60_000,
		retry: false,
		refetchOnWindowFocus: false,
	}));
</script>

<ReleaseFilesView raw={q.data} {kind} pending={q.isPending} error={q.error} />
