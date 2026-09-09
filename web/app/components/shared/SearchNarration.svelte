<script lang="ts">
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let {
		indexerCount,
		scopePending = false,
		onCancel,
	}: {
		// Enabled indexers this search fans out to. Undefined when the caller
		// cannot know — /indexers is admin-only, and a count nobody can verify
		// is worse than no count.
		indexerCount?: number;
		// The count is still in flight. The heading holds its line and says
		// nothing rather than stating the countless phrasing and then rewriting
		// itself once the number lands.
		scopePending?: boolean;
		onCancel?: () => void;
	} = $props();

	// The wait is worth narrating, not worth a stopwatch: under five seconds a
	// number is noise, past it the operator has started to wonder.
	let elapsed = $state(0);
	$effect(() => {
		const started = Date.now();
		const iv = setInterval(() => {
			elapsed = Math.round((Date.now() - started) / 1000);
		}, 1000);
		return () => clearInterval(iv);
	});
</script>

<!--
  The wait for a manual search. The endpoint answers once — there is no
  per-indexer progress to report and no rows to stream — so this states the
  scope of the work instead of drawing a placeholder table it cannot honour.
  One moving thing, not two: no spinner beside it.
-->
<div
	class="flex flex-col items-center justify-center gap-3 px-4 py-14 text-center"
	role="status"
	aria-live="polite"
>
	<div class="bars flex gap-[3px]" aria-hidden="true">
		<i></i>
		<i></i>
		<i></i>
		<i></i>
	</div>
	<p class="min-h-[1.15rem] text-[13.5px] font-semibold text-fg">
		{#if scopePending}&nbsp;{:else if indexerCount === undefined}{i18n.search_scope_indexers_unknown()}{:else}{i18n.search_scope_indexers({ count: indexerCount })}{/if}
	</p>
	<p class="max-w-[330px] text-xs leading-relaxed text-fg-subtle [text-wrap:pretty]">
		{i18n.search_ranked_hint()}
	</p>
	{#if elapsed >= 5 || onCancel}
		<div class="mt-0.5 flex items-center gap-2.5">
			{#if elapsed >= 5}
				<span class="tabular font-mono text-[11px] text-fg-faint">
					{i18n.common_elapsed_seconds({ seconds: elapsed })}
				</span>
			{/if}
			{#if onCancel}
				<button
					type="button"
					onclick={onCancel}
					class="inline-flex min-h-11 items-center rounded-md border border-border bg-bg-base px-3 text-xs font-medium text-fg-muted transition hover:bg-surface hover:text-fg lg:h-7 lg:min-h-0"
				>
					{i18n.common_cancel()}
				</button>
			{/if}
		</div>
	{/if}
</div>

<style>
	.bars i {
		width: 14px;
		height: 3px;
		border-radius: 9999px;
		background: var(--accent);
		opacity: 0.35;
	}
	@media (prefers-reduced-motion: no-preference) {
		.bars i {
			animation: hop 1.4s ease-in-out infinite;
		}
		.bars i:nth-child(2) {
			animation-delay: 0.16s;
		}
		.bars i:nth-child(3) {
			animation-delay: 0.32s;
		}
		.bars i:nth-child(4) {
			animation-delay: 0.48s;
		}
	}
	@keyframes hop {
		0%,
		100% {
			opacity: 0.35;
		}
		50% {
			opacity: 1;
		}
	}
</style>
