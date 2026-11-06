<script lang="ts">
	import { createMutation, createQuery, useQueryClient } from "@tanstack/svelte-query";
	import Select from "@components/forms/Select.svelte";
	import { api, errorText } from "@lib/api";
	import { toast } from "@lib/toast";
	import type { MusicAlbumEdition, MusicAlbumEditions } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Which release an album's track list comes from. Listing the editions is a
	// MusicBrainz call, so it waits for the button.
	let {
		albumId,
		onChanged,
	}: {
		albumId: number;
		onChanged?: () => void;
	} = $props();

	const qc = useQueryClient();
	let open = $state(false);

	const editions = createQuery<MusicAlbumEditions>(() => ({
		queryKey: ["album-editions", albumId],
		queryFn: () => api<MusicAlbumEditions>(`/music/albums/${albumId}/editions`),
		enabled: open,
		retry: false,
		refetchOnWindowFocus: false,
	}));

	const choose = createMutation<unknown, Error, string>(() => ({
		mutationFn: (release_mbid) =>
			api(`/music/albums/${albumId}/editions`, {
				method: "PUT",
				body: { release_mbid },
			}),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["music"] });
			qc.invalidateQueries({ queryKey: ["album-editions", albumId] });
			qc.invalidateQueries({ queryKey: ["artist-track-credits"] });
			toast.ok(i18n.music_edition_changed());
			onChanged?.();
		},
		onError: (e) => toast.err(errorText(e)),
	}));

	const editionLabel = (e: MusicAlbumEdition) =>
		[
			e.date,
			e.country,
			e.formats.join(" + "),
			e.tracks === 1
				? i18n.music_edition_tracks_one()
				: i18n.music_edition_tracks_other({ count: e.tracks }),
			e.title,
			e.disambiguation,
		]
			.filter(Boolean)
			.join(" · ");

	const options = $derived([
		{ value: "", label: i18n.music_edition_automatic() },
		...(editions.data?.editions ?? []).map((e) => ({
			value: e.mbid,
			label: editionLabel(e),
		})),
	]);
	const current = $derived(
		editions.data?.editions.find((e) => e.mbid === editions.data?.current),
	);
</script>

{#if open && editions.data}
	<div class="flex flex-col gap-1">
		<Select
			label={i18n.music_edition_label()}
			value={editions.data.chosen}
			{options}
			onChange={(v) => choose.mutate(v)}
			disabled={choose.isPending}
			searchable={options.length > 10}
			maxHeight={240}
		/>
		{#if current}
			<p class="text-xs text-fg-muted">
				{i18n.music_edition_current({ edition: editionLabel(current) })}
			</p>
		{/if}
	</div>
{:else if open && editions.isError}
	<p class="text-xs text-fg-muted">{errorText(editions.error)}</p>
{:else}
	<button
		type="button"
		disabled={open}
		onclick={() => (open = true)}
		class="text-left text-xs text-fg-muted underline-offset-2 hover:text-fg hover:underline disabled:opacity-60"
	>
		{i18n.music_edition_change()}
	</button>
{/if}
