<script lang="ts">
	import { Inbox } from "@lucide/svelte";
	import { groupReview } from "../../lib/requests-touch";
	import type { MediaRequest } from "../../lib/types";
	import RequestTouchRow from "./RequestTouchRow.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// The touch band has no status control on screen — search took that space — so
	// the list shows everything and states the split itself. Landing on a
	// pending-only filter would hide the decided requests the stat line directly
	// above is still counting, with nothing on screen to explain the gap.
	let {
		requests,
		busy = false,
		onOpen,
		onApprove,
		onReject,
	}: {
		requests: MediaRequest[];
		busy?: boolean;
		onOpen: (r: MediaRequest) => void;
		onApprove: (r: MediaRequest) => void;
		onReject: (r: MediaRequest) => void;
	} = $props();

	let groups = $derived(groupReview(requests));
</script>

<div class="lg:hidden">
	{#if requests.length === 0}
		<div
			class="flex flex-col items-center justify-center rounded-lg border border-dashed border-border bg-bg-card/40 py-14 text-center"
		>
			<Inbox class="mb-3 h-9 w-9 text-fg-faint" aria-hidden="true" />
			<p class="text-[15px] font-semibold text-fg">{i18n.requests_nothing_to_show()}</p>
			<p class="mt-1 max-w-[15rem] text-[13px] text-fg-subtle">
				{i18n.requests_none_match()}
			</p>
		</div>
	{:else}
		{#each [{ key: "pending", label: i18n.common_pending(), rows: groups.pending, count: true }, { key: "decided", label: i18n.common_decided(), rows: groups.decided, count: false }] as g (g.key)}
			{#if g.rows.length > 0}
				<div class="flex items-center gap-2.5 pb-2 pt-5 first:pt-0">
					<h2
						class="font-mono text-[11px] font-semibold uppercase tracking-[0.14em] text-fg-muted"
					>
						{g.label}
					</h2>
					{#if g.count}
						<span class="font-mono text-[11px] tabular-nums text-fg-faint">
							{g.rows.length}
						</span>
					{/if}
					<span class="h-px flex-1 bg-border" aria-hidden="true"></span>
				</div>

				<ul
					class="divide-y divide-border overflow-hidden rounded-lg border border-border bg-bg-elevated"
				>
					{#each g.rows as r (r.id)}
						<li>
							<RequestTouchRow
								request={r}
								reviewer
								wide
								{busy}
								{onOpen}
								{onApprove}
								{onReject}
							/>
						</li>
					{/each}
				</ul>
			{/if}
		{/each}
	{/if}
</div>
