<script lang="ts">
	import { BookOpen, Headphones } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import { auth } from "@lib/auth.svelte";
	import StatusPill from "@components/shared/StatusPill.svelte";
	import BookCover from "./BookCover.svelte";
	import LibraryActions from "@components/shared/LibraryActions.svelte";
	import { formatLabel, kindLabel, type FormatState, type ShelfItem } from "@lib/music-books";
	import { bookPosterUrl } from "@lib/posters";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// A cover on a shelf or in a kind's grid. A series is one card with two
	// sheets stacked behind it; a book shows which formats it follows, white
	// when that format is in the library, amber when it is wanted.
	let { item }: { item: ShelfItem } = $props();
	let canEdit = $derived(auth.canAddDirectly);
	let isSeries = $derived(item.type === "series");
	let hasFiles = $derived(isSeries ? (item.volumes_have ?? 0) > 0 : (item.formats ?? []).some((f) => f.state === "available"));

	let href = $derived(item.type === "series" ? `/books/series/${item.id}` : `/books/${item.id}`);
	let formats = $derived((item.formats ?? []).filter((f) => f.state !== "unmonitored"));
	const tone = (s: FormatState) =>
		s === "available" ? "text-white/90" : s === "downloading" ? "text-status-downloading" : "text-status-wanted";
	const stateText = (s: FormatState) =>
		s === "available" ? i18n.status_available() : s === "downloading" ? i18n.status_downloading() : i18n.status_wanted();
</script>

<div class="group relative">
<a
	{href}
	class="relative block rounded-lg transition duration-200 hover:scale-[1.02] focus:outline-none focus-visible:scale-[1.02]"
	title={item.title}
>
	{#if item.type === "series"}
		<div class="absolute inset-0 translate-x-[7px] -translate-y-[7px] rounded-lg border border-white/10 bg-bg-hover" aria-hidden="true"></div>
		<div class="absolute inset-0 translate-x-[3.5px] -translate-y-[3.5px] rounded-lg border border-white/10 bg-bg-card" aria-hidden="true"></div>
	{/if}
	<div
		class="relative overflow-hidden rounded-lg transition group-hover:shadow-[0_0_0_2px_var(--accent-ring),0_24px_64px_rgb(0_0_0_/0.55)] group-has-[:focus-visible]:shadow-[0_0_0_2px_var(--accent-ring),0_24px_64px_rgb(0_0_0_/0.55)]"
	>
		<BookCover src={bookPosterUrl(item.cover_id)} alt={i18n.common_poster_alt({ title: item.title })} />
		<div class="pointer-events-none absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/95 via-black/70 to-transparent px-3 pb-2.5 pt-12">
			<p class="truncate text-sm font-semibold text-white drop-shadow-[0_1px_3px_rgb(0_0_0_/0.95)]">{item.title}</p>
			<p class="truncate text-[11px] text-white/70">{item.author}</p>
			<p class="mt-0.5 flex items-center gap-1.5 truncate font-mono text-[11px] tracking-tight text-white/80">
				{#if item.kind !== "novel"}
					<span>{kindLabel(item.kind)}</span>
					<span class="text-white/50" aria-hidden="true">·</span>
				{/if}
				{#if item.type === "series"}
					<span>{i18n.books_volumes_of({ have: String(item.volumes_have ?? 0), total: String(item.volumes_out ?? 0) })}</span>
				{:else}
					<span>{item.year}</span>
					{#each formats as f (f.format)}
						<span class="text-white/50" aria-hidden="true">·</span>
						<span class={cn("inline-flex", tone(f.state))} title="{formatLabel(f.format)} · {stateText(f.state)}">
							{#if f.format === "ebook"}
								<BookOpen size={12} aria-hidden="true" />
							{:else}
								<Headphones size={12} aria-hidden="true" />
							{/if}
							<span class="sr-only">{formatLabel(f.format)}: {stateText(f.state)}</span>
						</span>
					{/each}
				{/if}
			</p>
		</div>
		<div class="absolute left-2 top-2">
			<StatusPill status={item.status} size="sm" live={item.status === "downloading"} />
		</div>
		{#if item.status === "downloading"}
			<div class="absolute inset-x-0 bottom-0 h-0.5 bg-white/10">
				<div class="h-full bg-status-downloading" style:width="{item.progress ?? 0}%"></div>
			</div>
		{/if}
	</div>
</a>
{#if canEdit}
	<div class="pointer-events-none absolute right-2 top-2 opacity-0 transition duration-200 group-hover:pointer-events-auto group-hover:opacity-100 group-has-[:focus-visible]:pointer-events-auto group-has-[:focus-visible]:opacity-100">
		<LibraryActions
			base={isSeries ? `/books/series/${item.id}` : `/books/${item.id}`}
			title={item.title}
			media="books"
			queryKey="books"
			backHref="/books"
			{hasFiles}
			profile={item.quality_profile}
			removeBody={isSeries ? i18n.books_series_remove_body() : i18n.books_remove_body()}
			filesLabel={isSeries ? i18n.books_series_delete_files_label() : i18n.books_delete_files_label()}
			variant="card"
		/>
	</div>
{/if}
</div>
