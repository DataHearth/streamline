<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import { Music, Plus, Search, X, Bookmark } from "@lucide/svelte";
	import { apiAllPages, errorText } from "@lib/api";
	import type { Paginated } from "@lib/api";
	import { auth } from "@lib/auth.svelte";
	import { fold } from "@lib/text";
	import { initials } from "@lib/people";
	import { INPUT_CLASS } from "@lib/form";
	import { cn } from "@lib/cn";
	import Skeleton from "@components/shared/Skeleton.svelte";
	import type { MusicArtist } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	const artistsQuery = createQuery<Paginated<MusicArtist>>(() => ({
		queryKey: ["music", "artists"],
		queryFn: () => apiAllPages<MusicArtist>("/music/artists"),
	}));

	let query = $state("");
	let all = $derived(artistsQuery.data?.items ?? []);
	let visible = $derived.by(() => {
		const needle = fold(query.trim());
		return all
			.filter((a) => needle === "" || fold(a.name).includes(needle))
			.toSorted((a, b) => a.sort_name.localeCompare(b.sort_name));
	});
	let filtering = $derived(query.trim() !== "");

	let metaLine = $derived(
		filtering
			? i18n.music_count_of({ visible: visible.length, total: all.length })
			: all.length === 1
				? i18n.music_artists_count_one({ count: 1 })
				: i18n.music_artists_count_other({ count: all.length }),
	);
</script>

<div class="flex flex-col gap-4 px-4 pb-4 pt-6 md:px-6">
	<header>
		<h1 class="text-2xl font-bold tracking-tight text-fg">{i18n.music_label()}</h1>
		<p class="mt-1 truncate text-sm text-fg-muted">{metaLine}</p>
	</header>
	<div class="flex flex-wrap items-center gap-3">
		<div class="relative min-w-0 flex-1 md:max-w-sm">
			<Search
				size={15}
				class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-fg-faint"
				aria-hidden="true"
			/>
			<input
				type="search"
				bind:value={query}
				placeholder={i18n.music_filter_placeholder()}
				aria-label={i18n.music_filter_placeholder()}
				class={cn(INPUT_CLASS, "pl-9 pr-9")}
			/>
			{#if query}
				<button
					type="button"
					onclick={() => (query = "")}
					aria-label={i18n.common_clear_search()}
					class="absolute right-2 top-1/2 -translate-y-1/2 rounded p-1 text-fg-muted transition hover:text-fg"
				>
					<X size={14} aria-hidden="true" />
				</button>
			{/if}
		</div>
		<p class="hidden text-sm text-fg-muted md:block">{metaLine}</p>
		{#if auth.canAddDirectly}
			<a
				href="/music/add"
				class="ml-auto inline-flex items-center gap-1.5 rounded-md bg-accent px-3.5 py-2 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover"
			>
				<Plus size={16} aria-hidden="true" />
				{i18n.music_add_artist()}
			</a>
		{/if}
	</div>

	{#if artistsQuery.isPending}
		<span class="sr-only" role="status">{i18n.common_loading()}</span>
		<div
			class="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6"
		>
			{#each Array(12) as _, i (i)}
				<Skeleton h={150} round="md" />
			{/each}
		</div>
	{:else if artistsQuery.isError}
		<p class="text-sm text-status-failed">
			{i18n.err_load_failed_detail({ reason: errorText(artistsQuery.error) })}
		</p>
	{:else if all.length === 0}
		<div
			class="rounded-lg border border-dashed border-border bg-bg-deep/40 p-10 text-center"
		>
			<Music size={28} class="mx-auto text-fg-faint" aria-hidden="true" />
			<p class="mt-3 text-sm text-fg">{i18n.music_none()}</p>
			{#if auth.canAddDirectly}
				<p class="mt-1 text-xs text-fg-muted">{i18n.music_none_help()}</p>
			{/if}
		</div>
	{:else if visible.length === 0}
		<p class="py-12 text-center text-sm text-fg-subtle">
			{i18n.common_no_matches()}
		</p>
	{:else}
		<ul
			class="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6"
		>
			{#each visible as artist (artist.id)}
				<li>
					<a
						href="/music/{artist.id}"
						class="flex h-full flex-col items-center gap-3 rounded-lg border border-border bg-bg-elevated p-4 text-center transition hover:border-border-strong"
					>
						<span
							class="grid h-20 w-20 place-items-center rounded-full bg-bg-card text-xl font-semibold text-fg-muted"
							aria-hidden="true"
						>
							{initials(artist.name)}
						</span>
						<span class="w-full min-w-0">
							<span class="block truncate text-sm font-semibold text-fg">
								{artist.name}
							</span>
							<span class="mt-0.5 block text-xs text-fg-muted">
								{artist.album_count === 1
									? i18n.music_albums_count_one({ count: 1 })
									: i18n.music_albums_count_other({
											count: artist.album_count,
										})}
							</span>
						</span>
						<span
							class={cn(
								"inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide",
								artist.monitored
									? "bg-accent-soft text-accent-text"
									: "bg-surface text-fg-muted",
							)}
						>
							<Bookmark
								size={10}
								fill={artist.monitored ? "currentColor" : "none"}
								aria-hidden="true"
							/>
							{artist.monitored
								? i18n.monitor_monitored()
								: i18n.monitor_unmonitored()}
						</span>
					</a>
				</li>
			{/each}
		</ul>
	{/if}
</div>
