<script lang="ts">
	import { ChevronRight } from "@lucide/svelte";
	import type { LedgerRow } from "@lib/dashboard";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The four libraries on the same columns: what each holds, what it still
	// wants, what is on its way — each in the unit that medium is counted in.
	// A row opens its library. Phone stacks each row; from md it is a table.
	let {
		rows,
		free,
		loading = false,
	}: {
		rows: LedgerRow[];
		// Admin only: system info is.
		free?: string;
		loading?: boolean;
	} = $props();

	const pct = (v: number, of: number) => (of > 0 ? Math.min(100, (v / of) * 100) : 0);
</script>

{#snippet bar(r: LedgerRow)}
	<div class="flex h-[3px] w-full overflow-hidden rounded-full bg-white/[0.08]" aria-hidden="true">
		<div class="bg-status-available" style:width="{pct(r.have, r.total)}%"></div>
		<div class="bg-status-downloading" style:width="{pct(r.moving, r.total)}%"></div>
		<div class="bg-status-wanted" style:width="{pct(r.wanted, r.total)}%"></div>
	</div>
{/snippet}

{#snippet figure(v: number, unit: string, token: string, label: string)}
	{#if v > 0}
		<span class="whitespace-nowrap">
			<span class="font-semibold tabular" style:color="var(--status-{token})">{v.toLocaleString()}</span>
			<span class="text-fg-subtle">{unit}</span>
			<span class="sr-only">{label}</span>
		</span>
	{:else}
		<span class="text-fg-faint" aria-label="0 {label}">—</span>
	{/if}
{/snippet}

<section aria-labelledby="ledger-title">
	<header class="mb-3 flex items-baseline justify-between gap-3">
		<h2 id="ledger-title" class="text-base font-semibold tracking-tight text-fg">{i18n.nav_library()}</h2>
		{#if free}
			<span class="font-mono text-[11.5px] text-fg-subtle">{i18n.dash_disk_free({ size: free })}</span>
		{/if}
	</header>

	<div class="overflow-hidden rounded-lg border border-border bg-bg-elevated">
		<div class="ledger-grid hidden border-b border-border px-5 py-2.5 md:grid" aria-hidden="true">
			<span></span>
			<span class="font-mono text-[10px] uppercase tracking-[0.12em] text-fg-faint">{i18n.dash_col_library()}</span>
			<span class="font-mono text-[10px] uppercase tracking-[0.12em] text-fg-faint">{i18n.status_wanted()}</span>
			<span class="font-mono text-[10px] uppercase tracking-[0.12em] text-fg-faint">{i18n.dash_col_queue()}</span>
			<span></span>
		</div>

		{#if loading}
			<ul aria-hidden="true">
				{#each Array.from({ length: 4 }) as _, i (i)}
					<li class="flex flex-col gap-3 px-4 py-4 md:px-5 {i > 0 ? 'border-t border-border' : ''}">
						<div class="h-3 w-40 rounded bg-bg-hover motion-safe:animate-pulse"></div>
						<div class="h-[3px] w-full rounded bg-bg-hover motion-safe:animate-pulse"></div>
					</li>
				{/each}
			</ul>
		{:else}
			<ul>
				{#each rows as r, i (r.key)}
					<li class={i > 0 ? "border-t border-border" : ""}>
						<a
							href={r.href}
							aria-label="{i18n.dash_open_library({ name: r.label })} · {r.count}"
							class="block px-4 py-3.5 transition hover:bg-surface focus:outline-none focus-visible:bg-surface md:px-5 md:py-4"
						>
							<!-- Phone -->
							<div class="md:hidden">
								<div class="flex items-center gap-2.5">
									<span class="h-2 w-2 shrink-0 rounded-full" style:background="var(--kind-{r.token})"></span>
									<span class="text-[14px] font-semibold text-fg">{r.label}</span>
									<span class="ml-auto truncate font-mono text-[12px] text-fg-muted">{r.count}</span>
									<ChevronRight size={16} class="shrink-0 text-fg-faint" aria-hidden="true" />
								</div>
								<div class="mt-2.5">{@render bar(r)}</div>
								<div class="mt-2 flex flex-wrap items-baseline gap-x-3 gap-y-0.5 font-mono text-[11px]">
									<!-- No column heads here, so the figures carry their own word;
									     the unit is the row's count above. -->
									{#if r.wanted > 0}
										<span class="whitespace-nowrap"><span class="font-semibold tabular text-status-wanted">{r.wanted.toLocaleString()}</span> <span class="text-fg-subtle">{i18n.lc_wanted()}</span></span>
									{/if}
									{#if r.moving > 0}
										<span class="whitespace-nowrap"><span class="font-semibold tabular text-status-downloading">{r.moving.toLocaleString()}</span> <span class="text-fg-subtle">{i18n.lc_queued()}</span></span>
									{/if}
									<span class="ml-auto truncate text-fg-subtle">{r.detail}</span>
								</div>
							</div>
							<!-- md and up -->
							<div class="ledger-grid hidden items-center md:grid">
								<div class="flex min-w-0 items-center gap-2.5">
									<span class="h-2 w-2 shrink-0 rounded-full" style:background="var(--kind-{r.token})"></span>
									<span class="truncate text-[14px] font-semibold text-fg">{r.label}</span>
								</div>
								<div class="min-w-0">
									<div class="flex min-w-0 items-baseline gap-2.5">
										<span class="whitespace-nowrap text-[13px] text-fg">{r.count}</span>
										<span class="truncate font-mono text-[11px] text-fg-subtle">{r.detail}</span>
									</div>
									<div class="mt-2">{@render bar(r)}</div>
								</div>
								<div class="font-mono text-[12px]">{@render figure(r.wanted, r.wantedUnit, "wanted", i18n.status_wanted())}</div>
								<div class="font-mono text-[12px]">{@render figure(r.moving, r.movingUnit, "downloading", i18n.dash_col_queue())}</div>
								<ChevronRight size={14} class="text-fg-faint" aria-hidden="true" />
							</div>
						</a>
					</li>
				{/each}
			</ul>
		{/if}
	</div>
</section>

<style>
	/* The tablet band is 698px of content once the rail and the gutters are
	   paid for: 112px figure columns leave the library column ~220px, enough
	   for "8 series" and its episode count side by side. */
	.ledger-grid {
		grid-template-columns: 112px minmax(0, 1fr) 112px 112px 14px;
		column-gap: 20px;
	}
	@media (min-width: 1024px) {
		.ledger-grid {
			grid-template-columns: 150px minmax(0, 1fr) 132px 132px 14px;
			column-gap: 24px;
		}
	}
</style>
