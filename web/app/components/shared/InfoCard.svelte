<script lang="ts" module>
	export type InfoLink = { text: string; href?: string; external?: boolean };
	export type InfoRow =
		| { group: string }
		// A string value is a fact and reads in mono, as the movie and series
		// cards have it; a list of links is people, in the body face, unless
		// `mono` says it is an id.
		| { label: string | InfoLink; value: string | InfoLink[]; tone?: string; mono?: boolean };
</script>

<script lang="ts">
	import { ExternalLink } from "@lucide/svelte";
	import { cn } from "@lib/cn";

	// The aside card of the movie and series overviews (Library, File), for
	// pages that build their rows from data. A row whose label is a link is a
	// person — a player before their instruments — and reads in the body face.
	let { id, title, rows, onNavigate }: { id: string; title: string; rows: InfoRow[]; onNavigate?: () => void } = $props();
</script>

{#snippet link(p: InfoLink)}
	{#if p.href}
		<a
			href={p.href}
			target={p.external ? "_blank" : undefined}
			rel={p.external ? "noopener noreferrer" : undefined}
			onclick={p.external ? undefined : onNavigate}
			class="inline-flex max-w-full items-center gap-1 rounded text-accent-text transition hover:text-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
		>
			<span class="truncate">{p.text}</span>
			{#if p.external}<ExternalLink size={11} class="shrink-0" aria-hidden="true" />{/if}
		</a>
	{:else}
		{p.text}
	{/if}
{/snippet}

<section class="rounded-lg border border-border bg-bg-elevated p-5" aria-labelledby={id}>
	<h4 {id} class="font-mono text-[11px] uppercase tracking-[0.14em] text-fg-faint">{title}</h4>
	<dl class="mt-3 grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-[12px]">
		{#each rows as r, i (i)}
			{#if "group" in r}
				<div class="col-span-2 mt-2 flex items-center gap-2.5 font-mono text-[9px] uppercase tracking-[0.12em] text-fg-faint">
					<span>{r.group}</span>
					<span class="h-px flex-1 bg-border" aria-hidden="true"></span>
				</div>
			{:else}
				<dt class={cn("min-w-0", typeof r.label === "string" ? "text-fg-subtle" : "truncate text-fg")}>
					{#if typeof r.label === "string"}{r.label}{:else}{@render link(r.label)}{/if}
				</dt>
				<dd class={cn("m-0 min-w-0 text-right [text-wrap:pretty]", r.tone ?? "text-fg", (typeof r.value === "string" ? r.mono !== false : r.mono === true) && "font-mono")}>
					{#if typeof r.value === "string"}
						{r.value}
					{:else}
						{#each r.value as p, j (j)}{#if j > 0}<span class="text-fg-faint">, </span>{/if}{@render link(p)}{/each}
					{/if}
				</dd>
			{/if}
		{/each}
	</dl>
</section>
