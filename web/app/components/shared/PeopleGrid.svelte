<script lang="ts" module>
	export type PersonTile = {
		key: string;
		name: string;
		role?: string;
		note?: string;
		photo?: string;
		href?: string;
		// A former member: there, but no longer in the line-up.
		dim?: boolean;
	};
</script>

<script lang="ts">
	import { cn } from "@lib/cn";
	import { initials } from "@lib/people";
	import Img from "./Img.svelte";

	// DetailCast's grid for the people on a record or a book. Most of them
	// have no page here: only someone who is in the library is a link, and a
	// link is the only name drawn in the accent, so "who else do I have" reads
	// at a glance. `list` is the compact form for a release's guests and an
	// artist's collaborators, where a wall of faces would outweigh the record.
	let {
		people,
		dense = false,
		round = false,
		layout = "grid",
	}: {
		people: PersonTile[];
		dense?: boolean;
		round?: boolean;
		layout?: "grid" | "list";
	} = $props();
</script>

{#snippet face(p: PersonTile, small: boolean)}
	{#if p.photo}
		<Img src={p.photo} alt="" loading="lazy" class="h-full w-full object-cover" />
	{:else}
		<span class={cn("grid h-full w-full place-items-center font-mono font-bold text-fg-faint", small ? "text-[12px]" : "text-2xl")}>
			{initials(p.name)}
		</span>
	{/if}
{/snippet}

{#if layout === "list"}
	<ul class="grid gap-x-4 gap-y-0.5 sm:grid-cols-2">
		{#each people as p (p.key)}
			<li class="min-w-0">
				<svelte:element
					this={p.href ? "a" : "div"}
					href={p.href}
					class={cn("group flex min-h-11 items-center gap-3 rounded-md py-1", p.href && "focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring")}
				>
					<span
						class={cn(
							"relative h-10 w-10 shrink-0 overflow-hidden bg-bg-card",
							round ? "rounded-full" : "rounded-md",
							p.href && "transition group-hover:ring-2 group-hover:ring-accent-ring",
						)}
					>
						{@render face(p, true)}
					</span>
					<span class="min-w-0">
						<span class={cn("block truncate text-[13px] font-medium", p.href ? "text-accent-text group-hover:text-accent" : "text-fg")}>{p.name}</span>
						{#if p.role || p.note}
							<span class="flex min-w-0 items-baseline gap-1.5 text-[11.5px] text-fg-subtle">
								{#if p.role}<span class="min-w-0 truncate">{p.role}</span>{/if}
								{#if p.role && p.note}<span class="text-fg-faint" aria-hidden="true">·</span>{/if}
								{#if p.note}<span class="shrink-0 font-mono text-[10.5px] text-fg-faint">{p.note}</span>{/if}
							</span>
						{/if}
					</span>
				</svelte:element>
			</li>
		{/each}
	</ul>
{:else}
	<ul class={cn("people-grid", dense ? "people-grid--dense" : "people-grid--full")}>
		{#each people as p (p.key)}
			<li class="min-w-0">
				<svelte:element
					this={p.href ? "a" : "div"}
					href={p.href}
					class={cn("group block min-w-0 text-center", p.href && "rounded-md focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring")}
				>
					<div
						class={cn(
							"relative mx-auto mb-2 aspect-square w-full max-w-[128px] overflow-hidden bg-bg-card",
							round ? "rounded-full" : "rounded-md",
							p.href && "transition group-hover:ring-2 group-hover:ring-accent-ring",
							p.dim && "opacity-55 grayscale",
						)}
					>
						{@render face(p, false)}
					</div>
					<div class={cn("truncate text-[12.5px] font-medium", p.href ? "text-accent-text group-hover:text-accent" : "text-fg")}>
						{p.name}
					</div>
					{#if p.role}
						<div class="mt-0.5 truncate text-[10.5px] text-fg-subtle">{p.role}</div>
					{/if}
					{#if p.note}
						<div class="mt-0.5 truncate font-mono text-[10px] text-fg-faint">{p.note}</div>
					{/if}
				</svelte:element>
			</li>
		{/each}
	</ul>
{/if}

<style>
	.people-grid {
		display: grid;
		gap: 16px;
	}
	.people-grid--dense {
		grid-template-columns: repeat(auto-fill, minmax(100px, 1fr));
	}
	.people-grid--full {
		grid-template-columns: repeat(auto-fill, minmax(128px, 1fr));
		gap: 18px;
	}
</style>
