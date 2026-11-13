<script lang="ts">
	import { BookOpen, Headphones } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import { auth } from "@lib/auth.svelte";
	import StatusPill from "@components/shared/StatusPill.svelte";
	import BookCover from "./BookCover.svelte";
	import SeriesCover from "./SeriesCover.svelte";
	import LibraryActions from "@components/shared/LibraryActions.svelte";
	import SelectBox from "@components/shared/SelectBox.svelte";
	import { longPress } from "@lib/long-press";
	import { bookPosterUrl } from "@lib/posters";
	import { formatLabel, kindLabel, type FormatState, type ShelfItem } from "@lib/music-books";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// A cover on a shelf or in a kind's grid. A series is one card fanning its
	// first volumes; a book shows which formats it follows, white when that
	// format is in the library, amber when it is wanted.
	// Selection as on PosterCard: opt-in through onSelect, the box behind hover
	// until something is selected, and a touch long press starts it.
	let {
		item,
		selected = false,
		selectionActive = false,
		onSelect,
		onLongPress,
	}: {
		item: ShelfItem;
		selected?: boolean;
		selectionActive?: boolean;
		onSelect?: (v: boolean) => void;
		onLongPress?: () => void;
	} = $props();
	let canEdit = $derived(auth.canAddDirectly);
	// While selecting, the card is a selection target rather than a link.
	// Capture phase: Routify's click scope sits below Svelte's delegated root.
	function onCardClick(e: MouseEvent) {
		if (!onSelect || !selectionActive) return;
		e.preventDefault();
		onSelect(!selected);
	}
	let isSeries = $derived(item.type === "series");
	let hasFiles = $derived(isSeries ? (item.volumes_have ?? 0) > 0 : (item.formats ?? []).some((f) => f.state === "available"));

	let href = $derived(item.type === "series" ? `/books/series/${item.id}` : `/books/${item.id}`);
	let formats = $derived((item.formats ?? []).filter((f) => f.state !== "unmonitored"));
	const tone = (s: FormatState) =>
		s === "available" ? "text-white/90" : s === "downloading" ? "text-status-downloading" : "text-status-wanted";
	const stateText = (s: FormatState) =>
		s === "available" ? i18n.status_available() : s === "downloading" ? i18n.status_downloading() : i18n.status_wanted();
</script>

<div class="group relative" use:longPress={{ onLongPress: () => onLongPress?.(), disabled: !onLongPress || selectionActive }}>
<a
	{href}
	onclickcapture={onCardClick}
	class="relative block rounded-lg transition duration-200 hover:scale-[1.02] focus:outline-none focus-visible:scale-[1.02]"
	title={item.title}
>
	<div
		class={cn(
			"relative overflow-hidden rounded-lg transition group-hover:shadow-[var(--shadow-card-hover)] group-has-[:focus-visible]:shadow-[var(--shadow-card-hover)]",
			isSeries && "bg-bg-deep",
			selected && "shadow-[0_0_0_2px_var(--accent)]",
		)}
	>
		{#if isSeries}
			<SeriesCover
				srcs={(item.cover_ids ?? []).map(bookPosterUrl)}
				alt={i18n.common_poster_alt({ title: item.title })}
				rounded="rounded-md"
			/>
		{:else}
			<BookCover src={bookPosterUrl(item.cover_id)} alt={i18n.common_poster_alt({ title: item.title })} />
		{/if}
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
		<div
			class={cn(
				"absolute left-2 top-2 transition",
				onSelect && (selectionActive ? "opacity-0" : "group-hover:opacity-0 group-has-[:focus-visible]:opacity-0"),
			)}
		>
			<StatusPill status={item.status} size="sm" live={item.status === "downloading"} />
		</div>
		{#if item.status === "downloading"}
			<div class="absolute inset-x-0 bottom-0 h-0.5 bg-white/10">
				<div class="h-full bg-status-downloading" style:width="{item.progress ?? 0}%"></div>
			</div>
		{/if}
	</div>
</a>
{#if onSelect}
	<div
		class={cn(
			"absolute left-2 top-2 z-10 transition",
			selectionActive
				? "opacity-100"
				: "pointer-events-none opacity-0 group-hover:pointer-events-auto group-hover:opacity-100 group-has-[:focus-visible]:pointer-events-auto group-has-[:focus-visible]:opacity-100",
		)}
	>
		<SelectBox
			variant="card"
			checked={selected}
			onChange={(v) => onSelect(v)}
			label={selected ? i18n.a11y_deselect_item({ title: item.title }) : i18n.a11y_select_item({ title: item.title })}
		/>
	</div>
{/if}
{#if canEdit && !selectionActive}
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
