<script lang="ts">
	import { cn } from "../../lib/cn";
	import type { CastMember } from "../../lib/types";

	let {
		cast,
		dense = false,
	}: {
		cast: CastMember[];
		dense?: boolean;
	} = $props();

	function initials(name: string): string {
		return name
			.split(/\s+/)
			.filter(Boolean)
			.map((p) => p[0])
			.join("")
			.slice(0, 2)
			.toUpperCase();
	}
</script>

{#if cast.length > 0}
	<div
		class={cn(
			"cast-grid",
			dense ? "cast-grid--dense" : "cast-grid--full",
		)}
	>
		{#each cast as member, i (i)}
			<svelte:element
				this={member.person_url ? "a" : "div"}
				href={member.person_url}
				target={member.person_url ? "_blank" : undefined}
				rel={member.person_url ? "noopener noreferrer" : undefined}
				class={cn(
					"group block min-w-0 text-center",
					member.person_url && "transition hover:opacity-90",
				)}
			>
				<div
					class={cn(
						"relative mb-2 aspect-square overflow-hidden rounded-md bg-bg-card",
						member.person_url &&
							"transition group-hover:ring-2 group-hover:ring-accent-ring",
					)}
				>
					{#if member.profile_url}
						<img
							src={member.profile_url}
							alt={member.name}
							loading="lazy"
							class="h-full w-full object-cover"
						/>
					{:else}
						<span
							class="grid h-full w-full place-items-center font-mono text-2xl font-bold text-fg-faint"
						>
							{initials(member.name)}
						</span>
					{/if}
				</div>
				<div class="truncate text-[12.5px] font-medium text-fg">
					{member.name}
				</div>
				{#if member.character}
					<div class="mt-0.5 truncate text-[10.5px] text-fg-subtle">
						{member.character}
					</div>
				{/if}
			</svelte:element>
		{/each}
	</div>
{:else}
	<div
		class="rounded-lg border border-dashed border-border bg-bg-elevated/40 py-10 text-center"
	>
		<p class="text-sm font-medium text-fg">No cast information</p>
		<p class="mt-1 text-xs text-fg-muted">
			Cast appears once TMDB credits are fetched for this title.
		</p>
	</div>
{/if}

<style>
	.cast-grid {
		display: grid;
		gap: 16px;
	}
	.cast-grid--dense {
		grid-template-columns: repeat(auto-fill, minmax(110px, 1fr));
	}
	.cast-grid--full {
		grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
		gap: 18px;
	}
</style>
