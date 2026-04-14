<script lang="ts">
	import { onMount } from "svelte";
	import {
		LayoutDashboard,
		Film,
		Activity,
		Settings,
	} from "@lucide/svelte";
	import { isActive as routifyIsActive } from "@roxi/routify";
	import { auth } from "../../lib/auth.svelte";
	import { cn } from "../../lib/cn";

	type IsActiveFn = (path: string) => boolean;
	let isActiveFn = $state<IsActiveFn>(() => false);
	onMount(() => routifyIsActive.subscribe((fn) => (isActiveFn = fn)));

	let tabs = $derived([
		{ label: "Dashboard", href: "/dashboard", icon: LayoutDashboard },
		{ label: "Movies", href: "/movies", icon: Film },
		{ label: "Activity", href: "/activity", icon: Activity },
		...(auth.isAdmin
			? [{ label: "Settings", href: "/settings", icon: Settings }]
			: []),
	]);
</script>

<nav
	class={cn(
		"fixed inset-x-0 bottom-0 z-40 grid min-h-14 border-t border-border bg-bg-elevated/95 pb-[env(safe-area-inset-bottom)] backdrop-blur-md saturate-150 lg:hidden",
		auth.isAdmin ? "grid-cols-4" : "grid-cols-3",
	)}
	aria-label="Primary"
>
	{#each tabs as tab (tab.href)}
		{@const active = isActiveFn(tab.href)}
		<a
			href={tab.href}
			aria-current={active ? "page" : undefined}
			class={cn(
				"relative flex flex-col items-center justify-center gap-1 px-2 pt-2.5 pb-3 text-[10.5px] transition-colors",
				active
					? "text-accent-text before:absolute before:inset-x-[18%] before:top-0 before:h-0.5 before:rounded-b-sm before:bg-accent"
					: "text-fg-subtle hover:text-fg-muted",
			)}
		>
			<tab.icon size={20} strokeWidth={active ? 2 : 1.6} />
			<span>{tab.label}</span>
		</a>
	{/each}
</nav>
