<script lang="ts">
	import { BookOpen, Check, Headphones } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import LangChip from "./LangChip.svelte";
	import { editionDetail, formatLabel, type BookFormat, type Edition } from "@lib/music-books";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Every edition of the book, both formats, the ones in use and the original
	// first. "Use" points that edition's format at it.
	let {
		editions,
		inUse,
		filter,
		onFilterChange,
		canEdit = false,
		onUse,
	}: {
		editions: Edition[];
		inUse: Record<BookFormat, number | null>;
		filter: "all" | BookFormat;
		onFilterChange: (f: "all" | BookFormat) => void;
		canEdit?: boolean;
		onUse: (e: Edition) => void;
	} = $props();

	const used = (e: Edition) => inUse[e.format] === e.id;
	let sorted = $derived(
		[...editions].sort((a, b) => Number(used(b)) - Number(used(a)) || Number(!!b.original) - Number(!!a.original)),
	);
	let shown = $derived(filter === "all" ? sorted : sorted.filter((e) => e.format === filter));
	let segs = $derived([
		{ key: "all" as const, label: i18n.common_all(), n: editions.length },
		{ key: "ebook" as const, label: formatLabel("ebook"), n: editions.filter((e) => e.format === "ebook").length },
		{ key: "audiobook" as const, label: formatLabel("audiobook"), n: editions.filter((e) => e.format === "audiobook").length },
	]);
	const useBtn =
		"inline-flex h-11 shrink-0 items-center rounded-lg border border-border bg-bg-elevated/80 px-3.5 text-[13px] font-medium text-fg transition hover:border-border-strong lg:h-8";
</script>

{#snippet inUseMark()}
	<span class="inline-flex items-center gap-1 whitespace-nowrap font-mono text-[11px] text-accent-text">
		<Check size={14} aria-hidden="true" />
		{i18n.books_in_use()}
	</span>
{/snippet}

{#snippet formatIcon(f: BookFormat, size: number)}
	{#if f === "ebook"}
		<BookOpen {size} aria-hidden="true" />
	{:else}
		<Headphones {size} aria-hidden="true" />
	{/if}
{/snippet}

<div class="mb-3 flex flex-wrap items-center justify-between gap-3">
	<div class="flex items-baseline gap-2">
		<h2 class="text-[15px] font-semibold text-fg">{i18n.books_editions()}</h2>
		<span class="font-mono text-[11px] text-fg-faint">{editions.length}</span>
	</div>
	<div class="inline-flex rounded-lg border border-border bg-bg-elevated p-0.5" role="group" aria-label={i18n.books_format()}>
		{#each segs as s (s.key)}
			<button
				type="button"
				aria-pressed={filter === s.key}
				onclick={() => onFilterChange(s.key)}
				class={cn(
					"inline-flex min-h-11 items-center gap-1.5 rounded-md px-3 text-[12.5px] font-medium transition lg:min-h-0 lg:py-1.5",
					filter === s.key ? "bg-surface-2 text-fg" : "text-fg-subtle hover:text-fg",
				)}
			>
				{s.label}
				<span class="font-mono text-[10.5px] text-fg-faint">{s.n}</span>
			</button>
		{/each}
	</div>
</div>

<div class="hidden overflow-hidden rounded-lg border border-border bg-bg-elevated/70 md:block">
	<table class="w-full border-collapse text-sm">
		<thead>
			<tr class="border-b border-border">
				{#each [i18n.common_language(), i18n.books_edition(), i18n.books_format(), i18n.common_details()] as h (h)}
					<th class="px-3 py-2.5 text-left font-mono text-[10px] font-medium uppercase tracking-[0.12em] text-fg-faint">{h}</th>
				{/each}
				<th class="px-3 py-2.5"><span class="sr-only">{i18n.books_use()}</span></th>
			</tr>
		</thead>
		<tbody>
			{#each shown as e (e.id)}
				<tr class="border-b border-border/60 last:border-b-0">
					<td class="px-3 py-2.5">
						<div class="flex items-center gap-1.5">
							<LangChip code={e.language} active={used(e)} />
							{#if e.original}
								<span class="font-mono text-[10px] uppercase tracking-[0.08em] text-fg-faint">{i18n.books_original()}</span>
							{/if}
						</div>
					</td>
					<td class="px-3 py-2.5">
						<p class="text-fg">{e.title}</p>
						<p class="font-mono text-[11px] text-fg-subtle">{e.publisher} · {e.year}</p>
					</td>
					<td class="px-3 py-2.5">
						<span class="inline-flex items-center gap-1.5 whitespace-nowrap text-[13px] text-fg-muted">
							{@render formatIcon(e.format, 14)}
							{formatLabel(e.format)}
						</span>
					</td>
					<td class="px-3 py-2.5 text-[12.5px] text-fg-subtle">{editionDetail(e)}</td>
					<td class="px-3 py-2.5 text-right">
						{#if used(e)}
							{@render inUseMark()}
						{:else if canEdit}
							<button type="button" class={useBtn} onclick={() => onUse(e)}>{i18n.books_use()}</button>
						{/if}
					</td>
				</tr>
			{/each}
		</tbody>
	</table>
</div>

<ol class="overflow-hidden rounded-lg border border-border bg-bg-elevated/70 md:hidden">
	{#each shown as e (e.id)}
		<li class="flex items-center gap-3 border-b border-border/60 px-3 py-3 last:border-b-0">
			<span class="text-fg-muted">{@render formatIcon(e.format, 16)}</span>
			<div class="min-w-0 flex-1">
				<div class="flex items-center gap-1.5">
					<LangChip code={e.language} active={used(e)} />
					{#if e.original}
						<span class="font-mono text-[10px] uppercase tracking-[0.08em] text-fg-faint">{i18n.books_original()}</span>
					{/if}
				</div>
				<p class="mt-1 truncate text-[13.5px] text-fg">{e.publisher} · {e.year}</p>
				<p class="truncate font-mono text-[11px] text-fg-subtle">{editionDetail(e)}</p>
			</div>
			{#if used(e)}
				{@render inUseMark()}
			{:else if canEdit}
				<button type="button" class={useBtn} onclick={() => onUse(e)}>{i18n.books_use()}</button>
			{/if}
		</li>
	{/each}
</ol>
