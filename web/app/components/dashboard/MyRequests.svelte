<script lang="ts">
	import LabelPill from "@components/shared/LabelPill.svelte";
	import { STATUS_META, groupMine, kindLabel } from "@lib/requests-touch";
	import { formatArrival } from "@lib/dates";
	import type { MediaRequest } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// A request_only member's "needs you": not work, the answer to what they
	// asked for. Waiting ones first, then the latest settled, three at most —
	// the requests page holds the rest.
	let { requests }: { requests: MediaRequest[] } = $props();

	let rows = $derived.by(() => {
		const g = groupMine(requests);
		return [...g.waiting, ...g.done].slice(0, 3);
	});
</script>

{#if rows.length > 0}
	<section class="overflow-hidden rounded-lg border border-border bg-bg-elevated md:max-w-[560px]" aria-labelledby="mine-title">
		<header class="flex items-baseline justify-between gap-3 px-4 pb-1 pt-3.5">
			<h2 id="mine-title" class="font-mono text-[10.5px] uppercase tracking-[0.14em] text-fg-faint">
				{i18n.dash_your_requests()}
			</h2>
			<a href="/requests" class="touch-hit text-[11.5px] text-fg-subtle transition hover:text-accent-text">
				{i18n.dash_see_all()} →
			</a>
		</header>
		<ul>
			{#each rows as r, i (r.id)}
				{@const meta = STATUS_META[r.status]}
				<li class={i > 0 ? "border-t border-border" : ""}>
					<a href="/requests" class="flex min-h-[52px] items-center gap-3 px-4 py-2.5 transition hover:bg-surface">
						<span class="min-w-0 flex-1">
							<span class="block truncate text-[14px] text-fg">{r.title}</span>
							<span class="block truncate font-mono text-[11px] text-fg-subtle">
								{kindLabel(r.media_type)} · {formatArrival(r.updated_at)}
							</span>
						</span>
						<LabelPill token={meta.token} label={meta.mine} size="md" variant="translucent" />
					</a>
				</li>
			{/each}
		</ul>
	</section>
{/if}
