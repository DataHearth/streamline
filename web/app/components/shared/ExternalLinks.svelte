<script lang="ts">
	// The hero's row of outbound links (lib/external-links.ts): the databases
	// that know the title by id, a hairline, then the review sites. A search
	// link carries a magnifier instead of the arrow, so a tap that opens a
	// results page rather than the title's own is not a surprise.
	import { ExternalLink as ExternalLinkIcon, Search } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import type { ExternalLink } from "@lib/external-links";
	import { m as i18n } from "@lib/paraglide/messages.js";

	let { links, class: className }: { links: ExternalLink[]; class?: string } = $props();

	let databases = $derived(links.filter((l) => l.group === "database"));
	let reviews = $derived(links.filter((l) => l.group === "reviews"));

	function labelFor(l: ExternalLink): string {
		return l.search && l.query
			? i18n.external_search_on({ site: l.label, title: l.query })
			: i18n.external_open_on({ site: l.label });
	}
</script>

{#snippet pill(l: ExternalLink)}
	<a
		href={l.href}
		target="_blank"
		rel="noopener noreferrer"
		aria-label={labelFor(l)}
		title={labelFor(l)}
		class="inline-flex h-7 items-center gap-1 rounded-full border border-border bg-black/30 px-2.5 text-[11px] font-medium text-fg-muted backdrop-blur-sm transition hover:border-accent/40 hover:text-fg focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-ring"
	>
		{l.label}
		{#if l.search}
			<Search size={10} aria-hidden="true" />
		{:else}
			<ExternalLinkIcon size={10} aria-hidden="true" />
		{/if}
	</a>
{/snippet}

{#if links.length > 0}
	<nav aria-label={i18n.external_links()} class={cn("flex flex-wrap items-center gap-1.5", className)}>
		{#each databases as l (l.site)}
			{@render pill(l)}
		{/each}
		{#if databases.length > 0 && reviews.length > 0}
			<span class="mx-1 h-4 w-px bg-border" aria-hidden="true"></span>
		{/if}
		{#each reviews as l (l.site)}
			{@render pill(l)}
		{/each}
	</nav>
{/if}
