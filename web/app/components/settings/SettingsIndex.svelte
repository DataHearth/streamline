<script lang="ts">
	import { ChevronRight, type SlidersHorizontal } from "@lucide/svelte";
	import { createSettingsNav } from "@lib/settings-nav.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The settings navigation below lg, where /settings is a page of its own:
	// one heading per group, one row per section, each with the count it owns.
	// The top bar carries the page's h1.
	const nav = createSettingsNav();
</script>

<p class="text-sm text-fg-muted">{i18n.settings_server_library()}</p>

<nav aria-label={i18n.settings_sections()} class="mt-4">
	{#each nav.groups as g (g.name)}
		<div class="flex items-center gap-2.5 pb-2 pt-5">
			<h2
				class="eyebrow text-[11px] text-fg-muted"
			>
				{g.name}
			</h2>
			<span class="h-px flex-1 bg-border" aria-hidden="true"></span>
		</div>
		<ul
			class="-mx-4 divide-y divide-border border-y border-border bg-bg-elevated sm:mx-0 sm:overflow-hidden sm:rounded-lg sm:border"
		>
			{#each g.items as it (it.path)}
				<li>
					{@render row(it.path, it.Icon, it.label, it.count)}
				</li>
			{/each}
		</ul>
	{/each}
</nav>

{#snippet row(
	path: string,
	Icon: typeof SlidersHorizontal,
	label: string,
	count?: number,
)}
	<a
		href={path}
		class="flex min-h-[56px] items-center gap-3 px-4 py-3 transition active:bg-bg-hover"
	>
		<Icon size={18} class="shrink-0 text-fg-muted" aria-hidden="true" />
		<span class="min-w-0 flex-1 truncate text-[15px] text-fg">{label}</span>
		{#if count !== undefined}
			<span class="shrink-0 font-mono text-xs tabular-nums text-fg-subtle">
				{count}
			</span>
		{/if}
		<ChevronRight size={16} class="shrink-0 text-fg-faint" aria-hidden="true" />
	</a>
{/snippet}
