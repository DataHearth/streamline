<script lang="ts">
	import ClampedText from "@components/shared/ClampedText.svelte";
	import type { Snippet } from "svelte";
	import { ArrowLeft } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import HeroBackdrop from "@components/shared/HeroBackdrop.svelte";
	import { provideHeroHold } from "@lib/hero-hold.svelte";

	// MovieDetailHero's frame for the music and books detail pages: blurred
	// backdrop, back link, art beside the text from md, stacked on a phone.
	let {
		backdrop,
		backHref,
		backLabel,
		cols,
		artWrap = "w-60",
		labelId,
		title,
		originalTitle,
		facts = [],
		meta = [],
		overview,
		art,
		extra,
		actions,
	}: {
		backdrop?: string;
		backHref: string;
		backLabel: string;
		// The md/lg grid template, e.g. "md:grid-cols-[180px_1fr] lg:grid-cols-[220px_1fr]".
		cols: string;
		// The art's width below md, where it sits centred over the text.
		artWrap?: string;
		labelId: string;
		title: string;
		originalTitle?: string;
		facts?: string[];
		meta?: string[];
		overview?: string;
		art: Snippet;
		extra?: Snippet;
		actions?: Snippet;
	} = $props();

	provideHeroHold();
</script>

<!-- Each part carries its dot in front, and the row is pulled left by one dot
     inside a clip: the part that starts a line has its dot clipped, so a wrap
     never leaves one hanging. -->
{#snippet dotted(parts: string[], klass: string)}
	<div class={cn("overflow-hidden", klass)}>
		<div class="-ml-5 flex flex-wrap gap-y-1">
			{#each parts as part, i (i)}
				<span class="relative pl-5 before:absolute before:left-0 before:w-5 before:text-center before:text-fg-faint before:content-['·']">{part}</span>
			{/each}
		</div>
	</div>
{/snippet}

<section class="relative" aria-labelledby={labelId}>
	<HeroBackdrop src={backdrop} />

	<div class="relative w-full px-4 pt-6 md:px-8">
		<a
			href={backHref}
			class="touch-hit inline-flex items-center gap-1.5 rounded-full border border-border bg-black/40 px-3 py-1.5 text-[11.5px] font-medium text-fg-muted backdrop-blur-sm transition hover:bg-black/60 hover:text-fg"
		>
			<ArrowLeft size={13} aria-hidden="true" />
			{backLabel}
		</a>
	</div>

	<div
		class={cn(
			"relative grid w-full items-center gap-5 px-4 pb-7 pt-6 md:gap-8 md:px-8 md:pb-14 md:pt-10 lg:gap-10 lg:pb-16",
			cols,
		)}
	>
		<div class={cn("relative mx-auto md:mx-0 md:w-auto", artWrap)}>
			{@render art()}
		</div>

		<div class="min-w-0 text-left">
			{#if facts.length > 0}
				{@render dotted(facts, "mb-3 font-mono text-[11px] uppercase tracking-[0.08em] text-fg-muted")}
			{/if}
			<h1
				id={labelId}
				class="text-[26px] font-bold leading-[1.05] tracking-tight text-fg md:text-4xl lg:text-5xl"
			>
				{title}
			</h1>
			{#if originalTitle}
				<p class="mt-1 text-sm italic text-fg-subtle">{originalTitle}</p>
			{/if}
			{#if meta.length > 0}
				{@render dotted(meta, "mt-3 font-mono text-xs text-fg-muted")}
			{/if}
			{#if overview}
				<ClampedText class="mt-4" text={overview} />
			{/if}
			{#if extra}
				{@render extra()}
			{/if}
			{#if actions}
				<div class="mt-5 flex flex-col gap-3">
					{@render actions()}
				</div>
			{/if}
		</div>
	</div>
</section>
