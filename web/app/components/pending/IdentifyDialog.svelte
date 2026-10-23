<script lang="ts">
	import { m as i18n } from "@lib/paraglide/messages.js";
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { api, errorText } from "@lib/api";
	import { toast } from "@lib/toast";
	import type { SeriesLookupResult, TMDBMovieResult } from "@lib/types";
	import AddMovieModal from "@components/movies/AddMovieModal.svelte";
	import AddSeriesModal from "@components/series/AddSeriesModal.svelte";
	import ImportMatchSheet from "@components/library/ImportMatchSheet.svelte";
	import Dialog from "@components/modals/Dialog.svelte";
	import type { MatchId } from "@lib/imports-touch";

	type Props = {
		open: boolean;
		id: number;
		// The release name, and what the server's parser made of it. The raw
		// name finds nothing on TMDB/TVDB, so the parsed one seeds the search.
		title: string;
		parsedTitle?: string;
		onClose: () => void;
	};
	let { open, id, title, parsedTitle, onClose }: Props = $props();

	type Kind = "movie" | "series" | "album" | "book";
	// A default for the first step, not a decision — the operator answers
	// before any search runs.
	let guess = $derived.by<Kind>(() => {
		if (/\b(epub|mobi|azw3?|pdf|cbz|cbr|m4b|audiobook)\b/i.test(title)) return "book";
		if (/\b(flac|alac|mp3|aac|ogg|opus)\b/i.test(title)) return "album";
		return /\bS\d{2}(E\d{2})?\b/i.test(title) ? "series" : "movie";
	});
	let kind = $state<Kind | null>(null);

	$effect(() => {
		if (!open) kind = null;
	});

	const qc = useQueryClient();

	const libraryKey: Record<Kind, string> = { movie: "movies", series: "series", album: "music", book: "books" };
	const identify = createMutation<void, Error, { kind: Kind; providerId: MatchId }>(() => ({
		mutationFn: (v) =>
			api<void>(`/activity/pending/${id}/identify`, {
				method: "POST",
				body: v.kind === "album" ? { kind: v.kind, mbid: v.providerId } : { kind: v.kind, provider_id: v.providerId },
			}),
		onSuccess: (_res, v) => {
			qc.invalidateQueries({ queryKey: ["activity", "pending"] });
			qc.invalidateQueries({ queryKey: [libraryKey[v.kind]] });
			toast.ok(i18n.pending_matched());
			kind = null;
			onClose();
		},
		onError: (e) => toast.err(errorText(e, i18n.pending_match_failed())),
	}));

	function onPickMovie(r: TMDBMovieResult) {
		identify.mutate({ kind: "movie", providerId: r.tmdb_id });
	}
	function onPickSeries(r: SeriesLookupResult) {
		identify.mutate({ kind: "series", providerId: r.tvdb_id });
	}

	function action(k: Kind, label: string) {
		return {
			label,
			variant: guess === k ? ("primary" as const) : ("ghost" as const),
			autofocus: guess === k,
			dismiss: false,
			onClick: () => (kind = k),
		};
	}

	let seed = $derived(parsedTitle || title);
	// The parsed title is the video parser's, which reads "1984" as a year and
	// cuts "George Orwell - 1984" to the author. The release name minus its
	// extension and bracketed tags searches MusicBrainz and Hardcover better.
	let creatorSeed = $derived(
		title
			.replace(/\.[a-z0-9]{2,4}$/i, "")
			.replace(/[[(][^\])]*[\])]/g, "")
			.trim(),
	);
</script>

<Dialog
	open={open && kind === null}
	title={i18n.pending_identify_title()}
	body={i18n.pending_identify_body()}
	actions={[
		action("movie", i18n.pending_identify_movie()),
		action("series", i18n.pending_identify_series()),
		action("album", i18n.pending_identify_album()),
		action("book", i18n.pending_identify_book()),
	]}
	{onClose}
/>

<!-- Movies and series pick through the add-flow's own TMDB/TVDB selector in
     pick mode; music and books have none, so they take the import review's
     MusicBrainz/Hardcover picker. Neither writes to the library until we ask. -->
{#if kind === "movie"}
	<AddMovieModal
		open={true}
		mode="pick"
		seedQuery={seed}
		onPick={onPickMovie}
		onClose={() => (kind = null)}
	/>
{:else if kind === "series"}
	<AddSeriesModal
		open={true}
		mode="pick"
		seedQuery={seed}
		onPick={onPickSeries}
		onClose={() => (kind = null)}
	/>
{:else if kind === "album" || kind === "book"}
	<ImportMatchSheet
		open={true}
		kind={kind === "album" ? "music" : "book"}
		dialog
		seed={creatorSeed}
		context={title}
		busy={identify.isPending}
		onPick={(matchId) => kind && identify.mutate({ kind, providerId: matchId })}
		onClose={() => (kind = null)}
	/>
{/if}
