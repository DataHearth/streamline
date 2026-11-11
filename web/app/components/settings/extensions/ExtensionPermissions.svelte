<script lang="ts">
	import { Check, Minus, Plus } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import { AUTH_LABEL, SERVICE_META, type ExtensionPermissions } from "@lib/extensions";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// What an extension may do, in the three groups the admin grants: the
	// addresses it answers on, the library data it can read, the services it
	// can call. Each line is plain words first; the address and the view's
	// name — what config.yaml carries — sit beside it for whoever checks the
	// file. A group with nothing in it is left out.
	//
	// `mark` is for the approval: what the new version adds (+), what is
	// already granted (✓), and what it no longer asks for (−).
	let {
		permissions,
		mark,
		class: cls = "",
	}: {
		permissions: ExtensionPermissions;
		mark?: "added" | "kept" | "dropped";
		class?: string;
	} = $props();

	const head = "font-mono text-[10.5px] font-semibold uppercase tracking-[0.14em] text-fg-faint";
	let ink = $derived(mark === "kept" ? "text-fg-muted" : mark === "dropped" ? "text-fg-subtle line-through decoration-fg-faint" : "text-fg");
</script>

<div class={cn("flex flex-col gap-3.5", cls)}>
	{#if permissions.mounts.length}
		<section>
			<h4 class={head}>{i18n.extensions_group_addresses()}</h4>
			<ul class="mt-1.5 flex flex-col gap-1.5">
				{#each permissions.mounts as m (m.path)}
					<li class="flex min-w-0 items-baseline gap-2">
						{@render marker()}
						<code class={cn("shrink-0 font-mono text-[12.5px]", ink)}>{m.path}</code>
						{#if m.auth}
							<span class="min-w-0 text-[12.5px] text-fg-subtle">{AUTH_LABEL[m.auth]()}</span>
						{/if}
					</li>
				{/each}
			</ul>
		</section>
	{/if}
	{#if permissions.views.length}
		<section>
			<h4 class={head}>{i18n.extensions_group_data()}</h4>
			<ul class="mt-1.5 flex flex-col gap-1.5">
				{#each permissions.views as v (v.name)}
					<li class="flex min-w-0 items-baseline gap-2">
						{@render marker()}
						<span class={cn("min-w-0 flex-1 text-[13px] leading-snug", ink)}>{v.description}</span>
						<code class="hidden shrink-0 font-mono text-[11px] text-fg-faint sm:inline">{v.name}</code>
					</li>
				{/each}
			</ul>
		</section>
	{/if}
	{#if permissions.services.length}
		<section>
			<h4 class={head}>{i18n.extensions_group_services()}</h4>
			<ul class="mt-1.5 flex flex-col gap-1.5">
				{#each permissions.services as s (s)}
					<li class="flex min-w-0 items-baseline gap-2">
						{@render marker()}
						<span class={cn("text-[13px]", ink)}>{SERVICE_META[s].label()}</span>
						<span class="min-w-0 text-[12.5px] text-fg-subtle">{SERVICE_META[s].help()}</span>
					</li>
				{/each}
			</ul>
		</section>
	{/if}
</div>

{#snippet marker()}
	{#if mark === "added"}
		<Plus size={12} strokeWidth={2.5} class="relative top-[1px] shrink-0 text-status-wanted" aria-label={i18n.extensions_new()} />
	{:else if mark === "kept"}
		<Check size={12} strokeWidth={2.5} class="relative top-[1px] shrink-0 text-fg-faint" aria-hidden="true" />
	{:else if mark === "dropped"}
		<Minus size={12} strokeWidth={2.5} class="relative top-[1px] shrink-0 text-fg-faint" aria-hidden="true" />
	{/if}
{/snippet}
