<script lang="ts">
	import { onMount } from "svelte";
	import {
		SlidersHorizontal,
		Search,
		Download,
		Cast,
		Gauge,
		Clock,
		Shield,
		KeyRound,
		Users,
	} from "@lucide/svelte";
	import { isActive as routifyIsActive, goto } from "@roxi/routify";
	import { createQuery } from "@tanstack/svelte-query";
	import { auth } from "../../lib/auth.svelte";
	import { api } from "../../lib/api";
	import { cn } from "../../lib/cn";
	import Select from "../forms/Select.svelte";
	import type { DownloadClient, Indexer, MediaServer } from "../../lib/types";

	type IsActiveFn = (path: string) => boolean;
	let isActiveFn = $state<IsActiveFn>(() => false);
	type GotoFn = (path: string) => void;
	let gotoFn = $state<GotoFn>(() => {});
	onMount(() => {
		const unsubActive = routifyIsActive.subscribe((fn) => (isActiveFn = fn));
		const unsubGoto = goto.subscribe((fn) => (gotoFn = fn));
		return () => {
			unsubActive();
			unsubGoto();
		};
	});

	let isAdmin = $derived(auth.user?.role === "admin");

	const indexers = createQuery<Indexer[]>(() => ({
		queryKey: ["indexers"],
		queryFn: () => api<Indexer[]>("/indexers"),
	}));
	const downloadClients = createQuery<DownloadClient[]>(() => ({
		queryKey: ["download-clients"],
		queryFn: () => api<DownloadClient[]>("/download-clients"),
	}));
	const mediaServers = createQuery<{ items: MediaServer[] }>(() => ({
		queryKey: ["media-servers"],
		queryFn: () => api<{ items: MediaServer[] }>("/media-servers"),
	}));

	let indexerCount = $derived(indexers.data?.length);
	let downloadClientCount = $derived(downloadClients.data?.length);
	let mediaServerCount = $derived(mediaServers.data?.items.length);

	type Item = {
		path: string;
		Icon: typeof SlidersHorizontal;
		label: string;
		count?: number | undefined;
	};
	type Group = { name: string; items: Item[] };

	let groups: Group[] = $derived.by(() => {
		const base: Group[] = [
			{
				name: "System",
				items: [
					{
						path: "/settings/general",
						Icon: SlidersHorizontal,
						label: "General",
					},
				],
			},
			{
				name: "Library",
				items: [
					{
						path: "/settings/quality-profiles",
						Icon: Gauge,
						label: "Quality profiles",
					},
				],
			},
			{
				name: "Connections",
				items: [
					{
						path: "/settings/indexers",
						Icon: Search,
						label: "Indexers",
						count: indexerCount,
					},
					{
						path: "/settings/download-clients",
						Icon: Download,
						label: "Download clients",
						count: downloadClientCount,
					},
					{
						path: "/settings/media-servers",
						Icon: Cast,
						label: "Media servers",
						count: mediaServerCount,
					},
				],
			},
			{
				name: "Automation",
				items: [
					{
						path: "/settings/schedules",
						Icon: Clock,
						label: "Schedules",
					},
				],
			},
		];
		if (isAdmin) {
			base.push({
				name: "Security",
				items: [
					{
						path: "/settings/auth",
						Icon: Shield,
						label: "Authentication",
					},
					{
						path: "/settings/oidc",
						Icon: KeyRound,
						label: "Single Sign-On",
					},
					{
						path: "/settings/users",
						Icon: Users,
						label: "Users",
					},
				],
			});
		}
		return base;
	});

	// Flat option list for the mobile Select jumper (forms/Select has no groups).
	let sectionOptions = $derived(
		groups.flatMap((g) =>
			g.items.map((it) => ({
				value: it.path,
				label: it.count !== undefined ? `${it.label} (${it.count})` : it.label,
			})),
		),
	);

	// Flat option list for the mobile <select> jumper.
	let activePath = $derived.by(() => {
		for (const g of groups) {
			for (const it of g.items) {
				if (isActiveFn(it.path)) return it.path;
			}
		}
		return groups[0]?.items[0]?.path ?? "/settings/general";
	});
</script>

<!-- Mobile: Select jumper. Keeps the section list reachable in one tap. -->
<div class="block md:hidden">
	<Select
		value={activePath}
		options={sectionOptions}
		onChange={(v) => gotoFn(v)}
		ariaLabel="Settings section"
	/>
</div>

<aside
	class="hidden shrink-0 self-start md:sticky md:top-20 md:block md:w-56"
	aria-label="Settings sections"
>
	<div class="mb-3.5 px-2">
		<div
			class="font-mono text-[9.5px] uppercase tracking-[0.18em] text-fg-faint"
		>
			Settings
		</div>
		<h2 class="mt-0.5 text-base font-semibold tracking-tight text-fg">
			Server &amp; library
		</h2>
	</div>
	<nav class="space-y-3.5 text-sm">
		{#each groups as g (g.name)}
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
