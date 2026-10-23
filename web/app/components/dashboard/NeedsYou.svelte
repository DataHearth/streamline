<script lang="ts" module>
	// Something waiting on the person looking. Held downloads and imports are
	// an admin's; requests are any reviewer's.
	export type NeedItem = {
		key: "held" | "requests" | "imports" | "failed";
		count: number;
		label: string;
		detail: string;
		href: string;
		// A --status-* name.
		token: string;
	};
</script>

<script lang="ts">
	import { ChevronRight, CircleX, FolderInput, Inbox, TriangleAlert } from "@lucide/svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	let { items }: { items: NeedItem[] } = $props();

	const ICONS = { held: TriangleAlert, requests: Inbox, imports: FolderInput, failed: CircleX };
	const tint = (t: string) => `background: color-mix(in srgb, var(--status-${t}) 14%, transparent); color: var(--status-${t})`;
</script>

{#if items.length > 0}
	<!-- Phone: a list, with room to say what each one is. From md: one line of
	     chips, the detail in the tooltip. -->
	<section class="overflow-hidden rounded-lg border border-border bg-bg-elevated md:hidden">
		<h2 class="px-4 pb-1 pt-3.5 font-mono text-[10.5px] uppercase tracking-[0.14em] text-fg-faint">
			{i18n.dash_needs_you()}
		</h2>
		<ul>
			{#each items as item, i (item.key)}
				{@const Icon = ICONS[item.key]}
				<li class={i > 0 ? "border-t border-border" : ""}>
					<a href={item.href} class="flex min-h-[52px] items-center gap-3 px-4 py-2.5 transition hover:bg-surface">
						<span class="grid h-8 w-8 shrink-0 place-items-center rounded-md" style={tint(item.token)}>
							<Icon size={16} aria-hidden="true" />
						</span>
						<span class="min-w-0 flex-1">
							<span class="block text-[14px] text-fg">
								<span class="font-mono font-semibold tabular">{item.count.toLocaleString()}</span>
								{item.label}
							</span>
							{#if item.detail}
								<span class="block truncate font-mono text-[11px] text-fg-subtle">{item.detail}</span>
							{/if}
						</span>
						<ChevronRight size={16} class="shrink-0 text-fg-faint" aria-hidden="true" />
					</a>
				</li>
			{/each}
		</ul>
	</section>

	<section class="hidden flex-wrap items-center gap-2 md:flex">
		<h2 class="mr-1.5 font-mono text-[10.5px] uppercase tracking-[0.14em] text-fg-subtle">
			{i18n.dash_needs_you()}
		</h2>
		{#each items as item (item.key)}
			{@const Icon = ICONS[item.key]}
			<a
				href={item.href}
				title={item.detail || undefined}
				class="inline-flex min-h-11 items-center gap-2 whitespace-nowrap rounded-full border border-border bg-bg-elevated/80 pl-3 pr-2.5 text-[13px] backdrop-blur transition hover:border-border-strong hover:bg-bg-hover focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring lg:h-9 lg:min-h-0"
			>
				<span class="inline-flex" style="color: var(--status-{item.token})"><Icon size={14} aria-hidden="true" /></span>
				<span class="font-mono font-semibold tabular text-fg">{item.count.toLocaleString()}</span>
				<span class="text-fg-muted">{item.label}</span>
				<ChevronRight size={14} class="text-fg-faint" aria-hidden="true" />
			</a>
		{/each}
	</section>
{/if}
