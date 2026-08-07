<script lang="ts">
	import { ChevronRight, SlidersHorizontal } from "@lucide/svelte";
	import { createSettingsNav } from "../../lib/settings-nav.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// The touch settings navigation: /settings is a destination below lg instead
	// of a redirect to General. Five group headings, eleven rows, each carrying
	// the count it owns — the numbers the old Select jumper flattened into
	// "Indexers (2)" and the group names it dropped entirely.
	const nav = createSettingsNav();
</script>

<header class="pb-1">
	<h1 class="text-2xl font-bold tracking-tight text-fg">
		{i18n.nav_settings()}
	</h1>
	<p class="mt-1 text-sm text-fg-muted">{i18n.settings_server_library()}</p>
</header>

<nav aria-label={i18n.settings_sections()} class="mt-4">
	{#each nav.groups as g (g.name)}
		<div class="flex items-center gap-2.5 pb-2 pt-5">
			<h2
				class="font-mono text-[11px] font-semibold uppercase tracking-[0.14em] text-fg-muted"
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
			<span class="shrink-0 font-mono text-xs tabular-nums text-fg-faint">
				{count}
			</span>
		{/if}
		<ChevronRight size={16} class="shrink-0 text-fg-faint" aria-hidden="true" />
	</a>
{/snippet}
