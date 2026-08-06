<script lang="ts">
	import { Inbox } from "@lucide/svelte";
	import { groupMine } from "../../lib/requests-touch";
	import type { MediaRequest } from "../../lib/types";
	import RequestTouchRow from "./RequestTouchRow.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// E2: a request_only member is not triaging, they are checking on something.
	// Two groups answer that, so the four status tabs and the stat strip both go
	// — four states over five rows is chrome with nothing to filter.
	let {
		requests,
		onOpen,
	}: {
		requests: MediaRequest[];
		onOpen: (r: MediaRequest) => void;
	} = $props();

	let groups = $derived(groupMine(requests));
</script>

<div class="lg:hidden">
	{#if requests.length === 0}
		<div
			class="flex flex-col items-center justify-center rounded-lg border border-dashed border-border bg-bg-card/40 py-14 text-center"
		>
			<Inbox class="mb-3 h-9 w-9 text-fg-faint" aria-hidden="true" />
			<p class="text-[15px] font-semibold text-fg">{i18n.requests_none_yet()}</p>
			<p class="mt-1 max-w-[15rem] text-[13px] text-fg-subtle">
				{i18n.requests_none_yet_hint()}
			</p>
		</div>
	{:else}
		{#each [{ key: "waiting", label: i18n.requests_waiting_review(), rows: groups.waiting, count: true }, { key: "done", label: i18n.common_done(), rows: groups.done, count: false }] as g (g.key)}
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
							<RequestTouchRow request={r} wide mine {onOpen} />
							<!-- The reason is the one thing a rejected requester came to read,
							     so it sits on the row rather than behind a tap. -->
							{#if r.status === "denied" && r.reason}
								<p
									class="ml-[34px] mr-4 border-l-2 border-status-failed/50 pb-3 pl-3 text-[12.5px] leading-relaxed text-fg-muted md:ml-9"
								>
									{r.reason}
								</p>
							{/if}
						</li>
					{/each}
				</ul>
			{/if}
		{/each}
	{/if}
</div>
