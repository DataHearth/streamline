<script lang="ts">
	import type { Snippet } from "svelte";
	import { ArrowLeft } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import Img from "@components/shared/Img.svelte";

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
		meta = [],
		overview,
		art,
		pills,
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
		meta?: string[];
		overview?: string;
		art: Snippet;
		pills?: Snippet;
		extra?: Snippet;
		actions?: Snippet;
	} = $props();
</script>

<section class="relative" aria-labelledby={labelId}>
	<div class="absolute inset-0 z-0 overflow-hidden bg-bg-deep">
		{#if backdrop}
			<Img
				src={backdrop}
				alt=""
				aria-hidden="true"
				class="h-full w-full scale-110 object-cover opacity-70 blur-md"
			/>
		{/if}
		<div class="hero-overlay absolute inset-0"></div>
	</div>

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
			{#if pills}
				<div class="mb-3 flex flex-wrap items-center gap-2">
					{@render pills()}
				</div>
			{/if}
			<h1
				id={labelId}
				class="text-[26px] font-bold leading-[1.05] tracking-tight text-fg md:text-4xl lg:text-5xl"
			>
				{title}
			</h1>
			{#if originalTitle}
				<p class="mt-1 text-sm italic text-fg-faint">{originalTitle}</p>
			{/if}
			{#if meta.length > 0}
				<div class="mt-3 flex flex-wrap items-center gap-2 font-mono text-xs text-fg-muted">
					{#each meta as part, i (i)}
						{#if i > 0}
							<span class="text-fg-faint" aria-hidden="true">·</span>
						{/if}
						<span>{part}</span>
					{/each}
				</div>
			{/if}
			{#if overview}
				<p
					class="mt-4 line-clamp-3 max-w-[680px] text-sm leading-relaxed text-fg-muted [text-wrap:pretty]"
				>
					{overview}
				</p>
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

<style>
	.hero-overlay {
		background-image: linear-gradient(
			180deg,
			rgb(11 11 16 / 0.3) 0%,
			rgb(11 11 16 / 0.7) 60%,
			var(--bg-deep) 100%
		);
	}
</style>
