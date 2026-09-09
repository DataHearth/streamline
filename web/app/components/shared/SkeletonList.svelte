<script lang="ts">
	import Skeleton from "./Skeleton.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let {
		variant = "row",
		count = 3,
	}: {
		variant?: "card" | "row" | "divided" | "field" | "panel" | "poster" | "media-row";
		count?: number;
	} = $props();

	// Identical rows read as a graphic. Uneven ones read as content that has not
	// arrived yet. Deterministic, so the placeholder does not reflow while it
	// waits.
	const W = ["46%", "62%", "38%", "54%", "70%", "42%"];
	const w = (i: number, o = 0) => W[(i + o) % W.length];
</script>

<!--
  Renders N placeholder blocks as SIBLINGS, with no wrapper: every call site
  already has the container the real content uses, so the skeleton inherits its
  grid, spacing and dividers instead of restating them and drifting.

  One sr-only status covers the set — each bar is aria-hidden, so a screen
  reader hears "Loading" once rather than counting rectangles.
-->
<span class="sr-only" role="status">{i18n.common_loading()}</span>

{#each Array(count) as _, i}
	{#if variant === "card"}
		<div
			class="flex flex-col gap-3 rounded-lg border border-border bg-bg-elevated p-5"
		>
			<div class="flex items-center gap-3">
				<Skeleton w="34px" h={34} round="md" />
				<div class="flex min-w-0 flex-1 flex-col gap-1.5">
					<Skeleton w={w(i)} h={11} />
					<Skeleton w="34%" h={9} />
				</div>
			</div>
			<Skeleton w="100%" h={9} />
			<Skeleton w={w(i, 3)} h={9} />
			<!-- The real cards all carry a row of actions under a divider. Without
			     it the placeholder is ~60px shorter than what replaces it and the
			     grid jumps as the data lands. -->
			<Skeleton w="100%" h={36} round="md" class="mt-1" />
		</div>
	{:else if variant === "row"}
		<div
			class="flex items-center gap-4 rounded-lg border border-border bg-bg-elevated p-4"
		>
			<Skeleton w="32px" h={32} round="md" />
			<div class="flex min-w-0 flex-1 flex-col gap-2">
				<Skeleton w={w(i)} h={12} />
				<Skeleton w={w(i, 2)} h={10} />
			</div>
			<Skeleton w="58px" h={20} round="full" />
		</div>
	{:else if variant === "divided"}
		<div class="flex items-center gap-4 border-b border-border px-5 py-4 last:border-0">
			<Skeleton w="26px" h={26} round="full" />
			<div class="flex min-w-0 flex-1 flex-col gap-1.5">
				<Skeleton w={w(i)} h={11} />
				<Skeleton w={w(i, 2)} h={9} />
			</div>
			<Skeleton w="72px" h={9} class="hidden sm:block" />
		</div>
	{:else if variant === "field"}
		<div class="rounded-lg border border-border bg-bg-elevated p-4">
			<div class="flex items-center gap-2">
				<Skeleton w="16px" h={16} round="md" />
				<Skeleton w="84px" h={9} />
			</div>
			<Skeleton w={w(i)} h={13} class="mt-3" />
		</div>
	{:else if variant === "poster"}
		<!-- PosterCard captions sit ON the poster, under a gradient, so the cell is
		     one 2/3 block with the caption lines inside its lower edge. -->
		<div class="relative aspect-[2/3] w-full animate-pulse overflow-hidden rounded-lg bg-white/[0.06] motion-reduce:animate-none">
			<div class="absolute inset-x-3 bottom-3 flex flex-col gap-1.5">
				<Skeleton w={w(i)} h={10} />
				<Skeleton w="38%" h={8} />
			</div>
		</div>
	{:else if variant === "media-row"}
		<!-- Mirrors MovieList's tr: checkbox, w-10 poster, title (+ original
		     title), year, status pill. The last two columns are container-gated in
		     the real table, so they are gated here on the same @2xl / @3xl. -->
		<div class="flex items-center gap-3 border-b border-border px-3 py-2 last:border-b-0">
			<Skeleton w="16px" h={16} />
			<Skeleton w="40px" h={60} />
			<div class="flex min-w-0 flex-1 flex-col gap-1.5">
				<Skeleton w={w(i)} h={11} />
				<Skeleton w={w(i, 2)} h={9} />
			</div>
			<Skeleton w="38px" h={10} />
			<Skeleton w="68px" h={20} round="full" />
			<Skeleton w="96px" h={10} class="hidden @3xl:block" />
			<Skeleton w="52px" h={10} class="hidden @2xl:block" />
		</div>
	{:else}
		<section class="rounded-lg border border-border bg-bg-card p-4">
			<Skeleton w="140px" h={11} />
			<Skeleton w={w(i, 4)} h={9} class="mt-2" />
			<div class="mt-4 flex flex-col gap-3">
				<Skeleton w="100%" h={34} round="md" class="max-w-md" />
				<Skeleton w="100%" h={34} round="md" class="max-w-md" />
			</div>
		</section>
	{/if}
{/each}
