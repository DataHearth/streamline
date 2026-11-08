<script lang="ts">
	import { ChevronRight } from "@lucide/svelte";
	import {
		KIND_HREF,
		KIND_LABEL,
		KIND_TOKEN,
		fmtCount,
		fmtSize,
		share,
		type StatsMedia,
	} from "@lib/stats";
	import { cn } from "@lib/cn";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// What each library holds and weighs. The bar under the size is the
	// medium's share of everything on disk, in its calendar kind colour — the
	// dot the dashboard ledger already gives each library. A row opens its
	// library. Phone stacks each row; from md it is a table.
	let { media }: { media: StatsMedia[] } = $props();

	let bytes = $derived(media.reduce((a, m) => a + m.bytes, 0));
	const head = "font-mono text-[10px] uppercase tracking-[0.12em] text-fg-faint";
</script>

<div class="min-w-0">
	<div class="media-grid hidden border-b border-border px-4 pb-2 md:grid md:px-5" aria-hidden="true">
		<span class={head}></span>
		<span class={cn(head, "text-right")}>{i18n.stats_titles()}</span>
		<span class={cn(head, "text-right")}>{i18n.common_files()}</span>
		<span class={head}>{i18n.stats_on_disk()}</span>
		<span class={cn(head, "text-right")}>{i18n.status_wanted()}</span>
		<span></span>
	</div>
	<ul>
		{#each media as m, i (m.kind)}
			<li class={i > 0 ? "border-t border-border" : ""}>
				<a
					href={KIND_HREF[m.kind]}
					class="block px-4 py-3 transition hover:bg-surface focus:outline-none focus-visible:bg-surface md:px-5"
				>
					<!-- Phone -->
					<div class="md:hidden">
						<div class="flex items-center gap-2.5">
							<span class="h-2 w-2 shrink-0 rounded-full" style:background="var(--kind-{KIND_TOKEN[m.kind]})"></span>
							<span class="text-[14px] font-semibold text-fg">{KIND_LABEL[m.kind]()}</span>
							<span class="ml-auto font-mono text-[12px] tabular-nums text-fg">{fmtSize(m.bytes)}</span>
							<ChevronRight size={16} class="shrink-0 text-fg-faint" aria-hidden="true" />
						</div>
						<div class="mt-2 h-[3px] overflow-hidden rounded-full bg-white/[0.08]" aria-hidden="true">
							<div class="h-full" style:width="{share(m.bytes, bytes)}%" style:background="var(--kind-{KIND_TOKEN[m.kind]})"></div>
						</div>
						<div class="mt-1.5 flex flex-wrap gap-x-3 font-mono text-[11px] text-fg-subtle">
							<span><span class="tabular-nums text-fg-muted">{fmtCount(m.titles)}</span> {i18n.stats_titles_lc()}</span>
							<span><span class="tabular-nums text-fg-muted">{fmtCount(m.files)}</span> {i18n.stats_files_lc()}</span>
							{#if m.wanted > 0}
								<span><span class="tabular-nums text-status-wanted">{fmtCount(m.wanted)}</span> {i18n.lc_wanted()}</span>
							{/if}
						</div>
					</div>
					<!-- md and up -->
					<div class="media-grid hidden items-center md:grid">
						<span class="flex min-w-0 items-center gap-2.5">
							<span class="h-2 w-2 shrink-0 rounded-full" style:background="var(--kind-{KIND_TOKEN[m.kind]})"></span>
							<span class="truncate text-[13.5px] font-semibold text-fg">{KIND_LABEL[m.kind]()}</span>
						</span>
						<span class="text-right font-mono text-[12.5px] tabular-nums text-fg">{fmtCount(m.titles)}</span>
						<span class="text-right font-mono text-[12.5px] tabular-nums text-fg-muted">{fmtCount(m.files)}</span>
						<span class="min-w-0">
							<span class="block font-mono text-[12.5px] tabular-nums text-fg">{fmtSize(m.bytes)}</span>
							<span class="mt-1.5 block h-[3px] overflow-hidden rounded-full bg-white/[0.08]" aria-hidden="true">
								<span class="block h-full" style:width="{share(m.bytes, bytes)}%" style:background="var(--kind-{KIND_TOKEN[m.kind]})"></span>
							</span>
						</span>
						<span class="text-right font-mono text-[12.5px] tabular-nums">
							{#if m.wanted > 0}
								<span class="text-status-wanted">{fmtCount(m.wanted)}</span>
							{:else}
								<span class="text-fg-faint" aria-hidden="true">—</span><span class="sr-only">0</span>
							{/if}
						</span>
						<ChevronRight size={14} class="text-fg-faint" aria-hidden="true" />
					</div>
				</a>
			</li>
		{/each}
	</ul>
</div>

<style>
	.media-grid {
		grid-template-columns: minmax(0, 1fr) 56px 56px minmax(0, 1.2fr) 56px 14px;
		column-gap: 12px;
	}
</style>
