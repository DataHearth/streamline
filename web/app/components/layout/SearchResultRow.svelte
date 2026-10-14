<script lang="ts">
	import { BookOpen, ChevronRight, Film, Music, Tv } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import { initials } from "@lib/people";
	import { posterUrl, tvPosterUrl } from "@lib/posters";
	import Poster from "@components/shared/Poster.svelte";
	import {
		isTitleItem,
		itemKindLabel,
		itemSubline,
		type SearchItem,
	} from "@lib/search-model.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";
	import Img from "@components/shared/Img.svelte";

	// The full-screen search's row, on phones and tablets. The palette keeps its
	// own — it carries a keyboard cursor and hover state this one has no use for.
	let {
		item,
		onpick,
	}: {
		item: SearchItem;
		onpick: (item: SearchItem) => void;
	} = $props();

	let isTitle = $derived(isTitleItem(item));
	// A person leads somewhere too, so the row ends in a chevron rather than a
	// kind label — the same affordance the titles get.
	let leadsAway = $derived(isTitle || item.kind === "person");
</script>

<button
	type="button"
	onclick={() => onpick(item)}
	class="flex min-h-14 w-full items-center gap-3 rounded-xl px-2.5 py-2 text-left transition-colors hover:bg-surface active:bg-surface"
>
	{#if isTitle && (item.kind === "movie" || item.kind === "series")}
		{@const MediaIcon = item.kind === "movie" ? Film : Tv}
		{@const poster =
			item.kind === "movie" ? posterUrl({ id: item.id }) : tvPosterUrl(item.id)}
		<div
			class="relative h-[45px] w-[30px] shrink-0 overflow-hidden rounded-md bg-surface-2 ring-1 ring-border"
		>
			<div class="absolute inset-0 grid place-items-center text-fg-muted">
				<MediaIcon size={14} aria-hidden="true" />
			</div>
			<Poster
				src={poster}
				alt={i18n.common_poster_alt({ title: item.label })}
				class="relative h-full w-full object-cover"
			/>
		</div>
	{:else if item.kind === "artist" || item.kind === "book"}
		<div
			class={cn(
				"relative shrink-0 overflow-hidden bg-surface-2 ring-1 ring-border",
				item.kind === "artist" ? "h-10 w-10 rounded-full" : "h-[45px] w-[30px] rounded-md",
			)}
		>
			<div class="absolute inset-0 grid place-items-center text-fg-muted">
				{#if item.kind === "artist"}
					<Music size={14} aria-hidden="true" />
				{:else}
					<BookOpen size={14} aria-hidden="true" />
				{/if}
			</div>
			{#if item.image}
				<Img src={item.image} alt="" class="relative h-full w-full object-cover" />
			{/if}
		</div>
	{:else if item.kind === "person"}
		<div
			class="relative h-10 w-10 shrink-0 overflow-hidden rounded-full bg-surface-2 ring-1 ring-border"
		>
			{#if item.profile_url}
				<Img
					src={item.profile_url}
					alt={item.label}
					loading="lazy"
					class="h-full w-full object-cover"
				/>
			{:else}
				<span
					class="grid h-full w-full place-items-center font-mono text-[11px] font-bold text-fg-muted"
				>
					{initials(item.label)}
				</span>
			{/if}
		</div>
	{:else if item.kind === "page" || item.kind === "action"}
		{@const Icon = item.icon}
		<div
			class="grid h-9 w-9 shrink-0 place-items-center rounded-lg bg-surface-2 text-fg-muted"
		>
			<Icon size={16} aria-hidden="true" />
		</div>
	{/if}

	<span class="min-w-0 flex-1">
		<span
			class="block truncate text-[14.5px] font-medium tracking-tight text-fg"
		>
			{item.label}
		</span>
		{#if item.kind === "movie" || item.kind === "series"}
			<span class="mt-0.5 block truncate font-mono text-[10.5px] text-fg-subtle">
				{item.year ? `${item.year} · ` : ""}{item.kind}
			</span>
		{:else if item.kind === "artist" || item.kind === "book"}
			<span class="mt-0.5 block truncate font-mono text-[10.5px] text-fg-subtle">
				{[itemSubline(item), itemKindLabel(item).toLowerCase()].filter(Boolean).join(" · ")}
			</span>
		{:else if item.kind === "person"}
			<span class="mt-0.5 block truncate font-mono text-[10.5px] text-fg-subtle">
				{item.credits === 1
					? i18n.person_credit_count_one({ count: item.credits })
					: i18n.person_credit_count_other({ count: item.credits })}
			</span>
		{/if}
	</span>

	{#if leadsAway}
		<ChevronRight size={18} class="shrink-0 text-fg-faint" aria-hidden="true" />
	{:else}
		<span
			class="shrink-0 font-mono text-[9.5px] uppercase tracking-[0.1em] text-fg-faint"
		>
			{itemKindLabel(item)}
		</span>
	{/if}
</button>
