<script lang="ts">
	import type { Snippet } from "svelte";

	type Props = {
		title?: string;
		subtitle?: string;
		eyebrow?: string;
		children: Snippet;
		footer?: Snippet;
	};

	let { title, subtitle, eyebrow, children, footer }: Props = $props();
</script>

<div
	class="relative isolate grid grid-cols-1 min-h-dvh place-items-center overflow-hidden bg-bg-deep px-4 py-10"
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
						class="font-mono text-[10px] uppercase tracking-[0.18em] text-fg-faint"
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
