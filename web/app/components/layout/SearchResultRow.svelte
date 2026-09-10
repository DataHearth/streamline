<script lang="ts">
	import { ChevronRight, Film, Tv } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { initials } from "../../lib/people";
	import { posterUrl, tvPosterUrl } from "../../lib/posters";
	import Poster from "../movies/Poster.svelte";
	import { itemKindLabel, type SearchItem } from "../../lib/search-model.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// One row for both touch surfaces: the phone screen at full size, the tablet
	// panel dense. The palette keeps its own row — it carries a keyboard cursor
	// and hover state this one has no use for.
	let {
		item,
		dense = false,
		onpick,
	}: {
		item: SearchItem;
		dense?: boolean;
		onpick: (item: SearchItem) => void;
	} = $props();

	let isTitle = $derived(item.kind === "movie" || item.kind === "series");
	// A person leads somewhere too, so the row ends in a chevron rather than a
	// kind label — the same affordance the titles get.
	let leadsAway = $derived(isTitle || item.kind === "person");
</script>

<button
	type="button"
	onclick={() => onpick(item)}
	class={cn(
		"flex w-full items-center gap-3 rounded-xl px-2.5 text-left transition-colors hover:bg-surface active:bg-surface",
		dense ? "min-h-12 py-1.5" : "min-h-14 py-2",
	)}
>
	{#if isTitle && (item.kind === "movie" || item.kind === "series")}
		{@const MediaIcon = item.kind === "movie" ? Film : Tv}
		{@const poster =
			item.kind === "movie" ? posterUrl({ id: item.id }) : tvPosterUrl(item.id)}
		<div
			class={cn(
				"relative shrink-0 overflow-hidden rounded-md bg-surface-2 ring-1 ring-border",
				dense ? "h-10 w-[27px]" : "h-[45px] w-[30px]",
			)}
		>
			<div class="absolute inset-0 grid place-items-center text-fg-muted">
				<MediaIcon size={14} aria-hidden="true" />
			</div>
			<Poster
				src={poster}
				alt="{item.label} poster"
				class="relative h-full w-full object-cover"
			/>
		</div>
	{:else if item.kind === "person"}
		<div
			class={cn(
				"relative shrink-0 overflow-hidden rounded-full bg-surface-2 ring-1 ring-border",
				dense ? "h-9 w-9" : "h-10 w-10",
			)}
		>
			{#if item.profile_url}
				<img
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
			class={cn(
				"grid shrink-0 place-items-center rounded-lg bg-surface-2 text-fg-muted",
				dense ? "h-8 w-8" : "h-9 w-9",
			)}
		>
			<Icon size={16} aria-hidden="true" />
		</div>
	{/if}

	<span class="min-w-0 flex-1">
		<span
			class={cn(
				"block truncate font-medium tracking-tight text-fg",
				dense ? "text-[13.5px]" : "text-[14.5px]",
			)}
		>
			{item.label}
		</span>
		{#if item.kind === "movie" || item.kind === "series"}
			<span class="mt-0.5 block truncate font-mono text-[10.5px] text-fg-subtle">
				{item.year ? `${item.year} · ` : ""}{item.kind}
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
