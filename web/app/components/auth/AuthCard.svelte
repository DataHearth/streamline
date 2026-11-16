<script lang="ts">
	import type { Snippet } from "svelte";

	type Props = {
		title?: string;
		subtitle?: string;
		eyebrow?: string;
		// Set on a page the app shell renders: the card then fills what is left
		// below the top bar (and above the bottom nav under md) rather than the
		// whole viewport, which there overflows #main by the bar's height and
		// scrolls a page with nothing to scroll to.
		inShell?: boolean;
		children: Snippet;
		footer?: Snippet;
	};

	let { title, subtitle, eyebrow, inShell = false, children, footer }: Props =
		$props();
</script>

<div
	class={[
		"relative isolate grid grid-cols-1 place-items-center overflow-hidden bg-bg-deep px-4 py-10",
		inShell
			? "min-h-[calc(100dvh-var(--top-bar-height)-5rem-env(safe-area-inset-bottom))] md:min-h-[calc(100dvh-var(--top-bar-height))]"
			: "min-h-dvh",
	]}
>
	<div
		aria-hidden="true"
		class="pointer-events-none absolute inset-x-0 top-0 -z-10 h-[60vh] bg-[radial-gradient(ellipse_at_50%_0%,var(--accent-soft),transparent_60%)]"
	></div>
	<div class="relative isolate w-full max-w-[420px]">
		<div
			class="rounded-2xl border border-border bg-bg-elevated/95 p-7 shadow-3 backdrop-blur-xl sm:p-9"
		>
			<header class="mb-6 flex flex-col items-center gap-3 text-center">
				<img
					src="/static/images/favicon-512.png"
					alt=""
					aria-hidden="true"
					class="h-12 w-12 rounded-xl shadow-2 ring-1 ring-border-strong"
				/>
				{#if eyebrow}
					<span
						class="font-mono text-[10px] uppercase tracking-[0.18em] text-fg-subtle"
					>
						{eyebrow}
					</span>
				{/if}
				{#if title}
					<h1 class="text-xl font-semibold tracking-tight text-fg">
						{title}
					</h1>
				{/if}
				{#if subtitle}
					<p class="-mt-1 text-sm text-fg-muted">{subtitle}</p>
				{/if}
			</header>

			{@render children()}
		</div>

		{#if footer}
			<footer class="mt-5 text-center text-sm text-fg-muted">
				{@render footer()}
			</footer>
		{/if}
	</div>
</div>
