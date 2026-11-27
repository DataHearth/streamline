<script lang="ts">
	import { Check, ChevronDown, Minus, Plus, TriangleAlert } from "@lucide/svelte";
	import { slide } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { twMerge } from "tailwind-merge";
	import {
		AUTH_LABEL,
		BUNDLE_LABEL,
		viewLabel,
		clientPath,
		FOLDER_META,
		SERVICE_META,
		hasFullAccess,
		type ExtensionBundle,
		type ExtensionPermissions,
		type ExtensionView,
	} from "@lib/extensions";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// What an extension may do, in the groups the admin grants: full access
	// when it holds ffmpeg, the addresses it answers on, the library data it
	// can read, the folders it can use, the services it can call. Each line is
	// plain words first; the address, the view's name and the folder's name —
	// what config.yaml carries — sit beside it for whoever checks the file. A
	// group with nothing in it is left out.
	//
	// `mark` is for the approval: what the new version adds (+), what is
	// already granted (✓), and what it no longer asks for (−).
	// `base` is the extension's path; mounts are relative to it and are shown
	// as the address a client uses.
	let {
		permissions,
		base,
		mark,
		class: cls = "",
	}: {
		permissions: ExtensionPermissions;
		base: string;
		mark?: "added" | "kept" | "dropped";
		class?: string;
	} = $props();

	const head = "eyebrow text-[10.5px] text-fg-faint";
	let ink = $derived(mark === "kept" ? "text-fg-muted" : mark === "dropped" ? "text-fg-subtle line-through decoration-fg-faint" : "text-fg");

	let full = $derived(hasFullAccess(permissions));
	let services = $derived(
		permissions.services.flatMap((s) => (s === "ffmpeg" ? [] : [{ id: s, meta: SERVICE_META[s] }])),
	);

	// A bundle's views fold into one line; the ones outside any bundle keep
	// their own, in the order the module lists them.
	let bundles = $derived.by(() => {
		const out: { bundle: ExtensionBundle; views: ExtensionView[] }[] = [];
		for (const v of permissions.views) {
			if (!v.bundle) continue;
			const found = out.find((b) => b.bundle === v.bundle);
			if (found) found.views.push(v);
			else out.push({ bundle: v.bundle, views: [v] });
		}
		return out;
	});
	let loose = $derived(permissions.views.filter((v) => !v.bundle));
	let openBundles = $state<Partial<Record<ExtensionBundle, boolean>>>({});
</script>

<div class={twMerge("flex flex-col gap-3.5", cls)}>
	{#if full}
		<section
			class={twMerge(
				"rounded-md border p-3",
				mark === "kept" || mark === "dropped" ? "border-border" : "border-status-failed/40 bg-status-failed/[0.08]",
			)}
		>
			<h4 class={twMerge("flex items-center gap-2 text-[13px] font-semibold", mark === "kept" ? "text-fg-muted" : mark === "dropped" ? "text-fg-subtle line-through decoration-fg-faint" : "text-status-failed")}>
				{@render marker()}
				<TriangleAlert size={15} class="shrink-0" aria-hidden="true" />
				{i18n.extensions_full_access_title()}
			</h4>
			<p class={twMerge("mt-1.5 text-[13px] leading-relaxed", mark === "kept" ? "text-fg-muted" : mark === "dropped" ? "text-fg-subtle line-through decoration-fg-faint" : "text-fg")}>
				{i18n.extensions_full_access_body()}
			</p>
		</section>
	{/if}
	{#if permissions.mounts.length}
		<section>
			<h4 class={head}>{i18n.extensions_group_addresses()}</h4>
			<ul class="mt-1.5 flex flex-col gap-1.5">
				{#each permissions.mounts as m (m.path)}
					<li class="flex min-w-0 items-baseline gap-2">
						{@render marker()}
						<code class={twMerge("shrink-0 font-mono text-[12.5px]", ink)}>{clientPath(base, m.path)}</code>
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
				{#each bundles as b (b.bundle)}
					<li>
						<button
							type="button"
							aria-expanded={!!openBundles[b.bundle]}
							onclick={() => (openBundles[b.bundle] = !openBundles[b.bundle])}
							class="touch-hit flex w-full min-w-0 items-baseline gap-2 text-left"
						>
							{@render marker()}
							<span class={twMerge("min-w-0 flex-1 text-[13px] leading-snug", ink)}>{BUNDLE_LABEL[b.bundle]()}</span>
							<span class="shrink-0 text-[12.5px] text-fg-subtle">
								{(b.views.length === 1 ? i18n.extensions_bundle_kinds_one : i18n.extensions_bundle_kinds_other)({ count: b.views.length })}
							</span>
							<ChevronDown size={13} class={twMerge("relative top-[2px] shrink-0 text-fg-faint transition-transform", openBundles[b.bundle] && "rotate-180")} aria-hidden="true" />
						</button>
						{#if openBundles[b.bundle]}
							<ul class="mb-1 ml-5 mt-1.5 flex flex-col gap-1" transition:slide={{ duration: 200, easing: cubicOut }}>
								{#each b.views as v (v.name)}
									<li class="flex min-w-0 flex-wrap items-baseline gap-x-2">
										<code class="font-mono text-[11.5px] text-fg-subtle">{v.name}</code>
										<span class="min-w-0 text-[12px] text-fg-faint">{viewLabel(v)}</span>
									</li>
								{/each}
							</ul>
						{/if}
					</li>
				{/each}
				{#each loose as v (v.name)}
					<li class="flex min-w-0 items-baseline gap-2">
						{@render marker()}
						<span class={twMerge("min-w-0 flex-1 text-[13px] leading-snug", ink)}>{viewLabel(v)}</span>
						<code class="hidden shrink-0 font-mono text-[11px] text-fg-faint sm:inline">{v.name}</code>
					</li>
				{/each}
			</ul>
		</section>
	{/if}
	{#if permissions.folders.length}
		<section>
			<h4 class={head}>{i18n.extensions_group_folders()}</h4>
			<ul class="mt-1.5 flex flex-col gap-1.5">
				{#each permissions.folders as f (f)}
					<li class="flex min-w-0 items-baseline gap-2">
						{@render marker()}
						<span class={twMerge("shrink-0 text-[13px]", ink)}>{FOLDER_META[f].label()}</span>
						<span class="min-w-0 flex-1 text-[12.5px] text-fg-subtle">
							· {FOLDER_META[f].access()}{#if FOLDER_META[f].help}&nbsp;({FOLDER_META[f].help?.()}){/if}
						</span>
						<code class="hidden shrink-0 font-mono text-[11px] text-fg-faint sm:inline">{f}</code>
					</li>
				{/each}
			</ul>
		</section>
	{/if}
	{#if services.length}
		<section>
			<h4 class={head}>{i18n.extensions_group_services()}</h4>
			<ul class="mt-1.5 flex flex-col gap-1.5">
				{#each services as s (s.id)}
					<li class="flex min-w-0 items-baseline gap-2">
						{@render marker()}
						<span class={twMerge("text-[13px]", ink)}>{s.meta.label()}</span>
						<span class="min-w-0 text-[12.5px] text-fg-subtle">{s.meta.help()}</span>
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
