<script lang="ts">
	import { cn } from "../../lib/cn";
	import { initials } from "../../lib/people";
	import type { CastMember } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let {
		cast,
		dense = false,
		external = false,
	}: {
		cast: CastMember[];
		dense?: boolean;
		// The lookup panels describe a title that is not in the library yet, from
		// inside a modal: their cast keeps the provider link rather than sending
		// the click to a person page, which would tear down the add flow.
		external?: boolean;
	} = $props();

	function memberHref(m: CastMember): string | undefined {
		if (external) return m.person_url;
		return m.person_id ? `/people/${m.person_id}` : undefined;
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
			{@const href = memberHref(member)}
			<svelte:element
				this={href ? "a" : "div"}
				{href}
				target={href && external ? "_blank" : undefined}
				rel={href && external ? "noopener noreferrer" : undefined}
				class={cn(
					"group block min-w-0 text-center",
					href && "transition hover:opacity-90",
				)}
			>
				<div
					class={cn(
						"relative mb-2 aspect-square overflow-hidden rounded-md bg-bg-card",
						href && "transition group-hover:ring-2 group-hover:ring-accent-ring",
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
		<p class="text-sm font-medium text-fg">{i18n.cast_none()}</p>
		<p class="mt-1 text-xs text-fg-muted">
			{i18n.cast_none_help()}
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
