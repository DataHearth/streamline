<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { api, errorText } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import type { SeriesLookupResult, TMDBMovieResult } from "../../lib/types";
	import AddMovieModal from "../movies/AddMovieModal.svelte";
	import AddSeriesModal from "../series/AddSeriesModal.svelte";
	import Dialog from "../modals/Dialog.svelte";

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

	// A season token in the release name means series; nothing else in a name
	// distinguishes the two, so it is a default for the first step, not a
	// decision — the operator answers before any search runs.
	let guess = $derived<"movie" | "series">(
		/\bS\d{2}(E\d{2})?\b/i.test(title) ? "series" : "movie",
	);
	let kind = $state<"movie" | "series" | null>(null);

	$effect(() => {
		if (!open) kind = null;
	});

	const qc = useQueryClient();

	const identify = createMutation<
		void,
		Error,
		{ kind: "movie" | "series"; providerId: number }
	>(() => ({
		mutationFn: (v) =>
			api<void>(`/activity/pending/${id}/identify`, {
				method: "POST",
				body: { kind: v.kind, provider_id: v.providerId },
			}),
		onSuccess: (_res, v) => {
			qc.invalidateQueries({ queryKey: ["activity", "pending"] });
			qc.invalidateQueries({ queryKey: [v.kind === "movie" ? "movies" : "series"] });
			toast.ok("Matched. Review it and import when you're ready.");
			kind = null;
			onClose();
		},
		onError: (e) => toast.err(errorText(e, "Could not match that download")),
	}));

	function onPickMovie(r: TMDBMovieResult) {
		identify.mutate({ kind: "movie", providerId: r.tmdb_id });
	}
	function onPickSeries(r: SeriesLookupResult) {
		identify.mutate({ kind: "series", providerId: r.tvdb_id });
	}

	let seed = $derived(parsedTitle || title);
</script>

<Dialog
	open={open && kind === null}
	title="What is this download?"
	body="Pick the title it belongs to. It is added to your library if it isn't there yet, and this download is matched to it — nothing is imported until you say so."
	actions={[
		{
			label: "A movie",
			variant: guess === "movie" ? "primary" : "ghost",
			autofocus: guess === "movie",
			dismiss: false,
			onClick: () => (kind = "movie"),
		},
		{
			label: "A series",
			variant: guess === "series" ? "primary" : "ghost",
			autofocus: guess === "series",
			dismiss: false,
			onClick: () => (kind = "series"),
		},
	]}
	{onClose}
/>

<!-- The picker is the add-flow's own TMDB/TVDB selector in pick mode: same
     search and detail panel, and no library write until we ask for one. -->
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
{/if}
