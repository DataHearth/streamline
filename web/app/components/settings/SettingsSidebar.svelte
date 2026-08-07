<script lang="ts">
	import { onMount } from "svelte";
	import { SlidersHorizontal } from "@lucide/svelte";
	import { isActive as routifyIsActive } from "@roxi/routify";
	import { cn } from "../../lib/cn";
	import { createSettingsNav } from "../../lib/settings-nav.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// Desktop only, from lg. Below that the section list is a page of its own
	// (SettingsIndex at /settings) rather than a column competing with the
	// content for the same width — at 834 the old md sidebar left the content
	// 430px, which is less than a phone in landscape gets.
	//
	// The <Select> jumper this used to render below md is gone with it: it
	// flattened five groups and eleven destinations into one native menu, and
	// the index page shows the same structure the sidebar does.
	type IsActiveFn = (path: string) => boolean;
	let isActiveFn = $state<IsActiveFn>(() => false);
	onMount(() => routifyIsActive.subscribe((fn) => (isActiveFn = fn)));

	const nav = createSettingsNav();
</script>

<aside
	class="hidden shrink-0 self-start lg:sticky lg:top-20 lg:block lg:w-56"
	aria-label={i18n.settings_sections()}
>
	<div class="mb-3.5 px-2">
		<div
			class="font-mono text-[9.5px] uppercase tracking-[0.18em] text-fg-faint"
		>
			{i18n.nav_settings()}
		</div>
		<h2 class="mt-0.5 text-base font-semibold tracking-tight text-fg">
			{i18n.settings_server_library()}
		</h2>
	</div>
	<nav class="space-y-3.5 text-sm">
		{#each nav.groups as g (g.name)}
			<div>
				<div
					class="px-2.5 pb-1.5 font-mono text-[9.5px] uppercase tracking-[0.14em] text-fg-faint"
				>
					{g.name}
				</div>
				<div class="space-y-px">
					{#each g.items as it (it.path)}
						{@render link(it.path, it.Icon, it.label, it.count)}
					{/each}
				</div>
			</div>
		{/each}
	</nav>
</aside>

{#snippet link(
	path: string,
	Icon: typeof SlidersHorizontal,
	label: string,
	count?: number,
)}
	{@const active = isActiveFn(path)}
	<a
		href={path}
		aria-current={active ? "page" : undefined}
		class={cn(
			"group relative flex items-center gap-2.5 rounded-md px-2.5 py-2 text-sm transition-colors duration-150",
			active
				? "bg-accent-soft text-accent-text"
				: "text-fg-muted hover:bg-surface hover:text-fg",
		)}
	>
		{#if active}
			<span
				aria-hidden="true"
				class="absolute -left-2 top-1/2 h-4 w-[3px] -translate-y-1/2 rounded-r bg-accent"
			></span>
		{/if}
		<Icon
			size={16}
			class={cn("shrink-0", active && "text-accent-text")}
			aria-hidden="true"
		/>
		<span class="flex-1 truncate">{label}</span>
		{#if count !== undefined}
			<span
				class={cn(
					"inline-flex min-w-5 items-center justify-center rounded-full px-1.5 font-mono text-[10px] font-semibold",
					active ? "text-accent-text/80" : "text-fg-faint",
				)}
			>
				{count}
			</span>
		{/if}
	</a>
{/snippet}
