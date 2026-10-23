<script lang="ts" module>
	export type ToolbarTab = { key: string; label: string; count?: number; dot?: string };
	export type ToolbarOption = { key: string; label: string; count?: number };
	export type ToolbarFacet = {
		key: string;
		label: string;
		icon?: any;
		value: string;
		options: ToolbarOption[];
		onChange: (v: string) => void;
	};
</script>

<script lang="ts">
	import { ChevronDown, Search, SlidersHorizontal, X } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import { dragScroll } from "@lib/drag-scroll";
	import DropdownMenu from "@components/shared/DropdownMenu.svelte";
	import DropdownOption from "@components/shared/DropdownOption.svelte";
	import MediaFilterSheet from "@components/shared/MediaFilterSheet.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The music and books toolbar: the library toolbars' vocabulary — a status
	// menu, facet menus, the filter field and a sort — without the parts these
	// pages have no use for yet (grid/list switch, selection). Below md it is
	// the movies phone row: status chips plus the filter sheet.
	let {
		statusLabel,
		tabs,
		tab,
		onTabChange,
		facets = [],
		query,
		onQueryChange,
		placeholder,
		sorts,
		sort,
		onSortChange,
		onReset,
	}: {
		statusLabel: string;
		tabs: ToolbarTab[];
		tab: string;
		onTabChange: (key: string) => void;
		facets?: ToolbarFacet[];
		query: string;
		onQueryChange: (q: string) => void;
		placeholder: string;
		sorts: ToolbarOption[];
		sort: string;
		onSortChange: (key: string) => void;
		onReset: () => void;
	} = $props();

	let menu = $state<string | null>(null);
	let anchors = $state<Record<string, HTMLButtonElement | null>>({});
	let sheetOpen = $state(false);

	let current = $derived(tabs.find((t) => t.key === tab) ?? tabs[0]);
	let currentSort = $derived(sorts.find((s) => s.key === sort) ?? sorts[0]);
	let activeCount = $derived(
		(query ? 1 : 0) +
			(tab !== tabs[0]?.key ? 1 : 0) +
			facets.filter((f) => f.value !== f.options[0]?.key).length,
	);
	const toggle = (key: string) => (menu = menu === key ? null : key);
	const close = () => (menu = null);

	const trigger =
		"inline-flex h-9 shrink-0 items-center gap-2 whitespace-nowrap rounded-lg border border-border bg-bg-elevated px-3 text-[13px] font-semibold text-fg transition hover:border-border-strong focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring";
	const phoneChip =
		"inline-flex h-11 shrink-0 items-center gap-1.5 rounded-full border px-3.5 text-[13px] font-medium transition";
	const chipOn = "border-accent-line bg-accent-soft text-accent-text";
	const chipOff = "border-border bg-surface text-fg-muted";
</script>

{#snippet facetChips()}
	{#each facets as f (f.key)}
		<div class="pt-5">
			<div class="mb-2.5 font-mono text-[10.5px] uppercase tracking-[0.12em] text-fg-faint">
				{f.label}
			</div>
			<div class="flex flex-wrap gap-2">
				{#each f.options as o (o.key)}
					<button
						type="button"
						aria-pressed={f.value === o.key}
						onclick={() => f.onChange(o.key)}
						class={cn(phoneChip, f.value === o.key ? chipOn : chipOff)}
					>
						{o.label}
						{#if o.count != null}
							<span class="font-mono text-[10.5px] tabular opacity-70">{o.count}</span>
						{/if}
					</button>
				{/each}
			</div>
		</div>
	{/each}
{/snippet}

<div class="hidden w-full flex-wrap items-center gap-2 px-6 pt-3 md:flex">
	<button
		bind:this={anchors.status}
		type="button"
		class={trigger}
		aria-haspopup="listbox"
		aria-expanded={menu === "status"}
		aria-label="{statusLabel}: {current?.label}"
		onclick={() => toggle("status")}
	>
		<span class={cn("h-2 w-2 rounded-full", current?.dot ?? "bg-fg-subtle")} aria-hidden="true"></span>
		{current?.label}
		{#if current?.count != null}
			<span class="font-mono text-[10.5px] font-normal tabular text-fg-faint">{current.count}</span>
		{/if}
		<ChevronDown size={14} class="text-fg-subtle" aria-hidden="true" />
	</button>
	<DropdownMenu open={menu === "status"} anchor={anchors.status ?? null} onClose={close} ariaLabel={statusLabel}>
		{#each tabs as t (t.key)}
			<DropdownOption
				label={t.label}
				count={t.count}
				dot={t.dot ?? "bg-fg-subtle"}
				selected={t.key === tab}
				onSelect={() => {
					onTabChange(t.key);
					close();
				}}
			/>
		{/each}
	</DropdownMenu>

	{#each facets as f (f.key)}
		{@const value = f.options.find((o) => o.key === f.value) ?? f.options[0]}
		<button
			bind:this={anchors[f.key]}
			type="button"
			class={cn(trigger, f.value !== f.options[0]?.key && "border-accent-line text-accent-text")}
			aria-haspopup="listbox"
			aria-expanded={menu === f.key}
			aria-label="{f.label}: {value?.label}"
			onclick={() => toggle(f.key)}
		>
			{#if f.icon}
				<f.icon size={15} class="text-fg-subtle" aria-hidden="true" />
			{/if}
			<span class="max-w-[160px] truncate">{value?.label}</span>
			<ChevronDown size={14} class="text-fg-subtle" aria-hidden="true" />
		</button>
		<DropdownMenu open={menu === f.key} anchor={anchors[f.key] ?? null} onClose={close} ariaLabel={f.label}>
			{#each f.options as o (o.key)}
				<DropdownOption
					label={o.label}
					count={o.count}
					selected={o.key === f.value}
					onSelect={() => {
						f.onChange(o.key);
						close();
					}}
				/>
			{/each}
		</DropdownMenu>
	{/each}

	<label
		class="flex h-9 min-w-[180px] flex-1 basis-[220px] items-center gap-2 rounded-lg border border-border bg-bg-elevated px-3 transition focus-within:border-accent-line"
	>
		<Search size={16} class="shrink-0 text-fg-faint" aria-hidden="true" />
		<input
			type="search"
			value={query}
			oninput={(e) => onQueryChange(e.currentTarget.value)}
			{placeholder}
			aria-label={placeholder}
			class="min-w-0 flex-1 bg-transparent text-[13px] text-fg outline-none placeholder:text-fg-faint [&::-webkit-search-cancel-button]:hidden"
		/>
		{#if query}
			<button
				type="button"
				onclick={() => onQueryChange("")}
				aria-label={i18n.common_clear_search()}
				class="grid h-6 w-6 place-items-center rounded text-fg-subtle transition hover:text-fg"
			>
				<X size={13} aria-hidden="true" />
			</button>
		{/if}
	</label>

	<button
		bind:this={anchors.sort}
		type="button"
		class={trigger}
		aria-haspopup="listbox"
		aria-expanded={menu === "sort"}
		aria-label="{i18n.filter_sort()}: {currentSort?.label}"
		onclick={() => toggle("sort")}
	>
		{currentSort?.label}
		<ChevronDown size={14} class="text-fg-subtle" aria-hidden="true" />
	</button>
	<DropdownMenu open={menu === "sort"} anchor={anchors.sort ?? null} onClose={close} align="end" ariaLabel={i18n.filter_sort()}>
		{#each sorts as s (s.key)}
			<DropdownOption
				label={s.label}
				selected={s.key === sort}
				onSelect={() => {
					onSortChange(s.key);
					close();
				}}
			/>
		{/each}
	</DropdownMenu>
</div>

<div class="sticky top-16 z-20 bg-bg-deep/85 backdrop-blur-md md:hidden">
	<div class="flex items-center gap-2 px-4 py-2">
		<nav
			use:dragScroll
			aria-label={statusLabel}
			class="flex min-w-0 flex-1 items-center gap-2 overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
		>
			{#if query}
				<span class={cn(phoneChip, chipOn)}>
					“{query}”
					<button
						type="button"
						onclick={() => onQueryChange("")}
						aria-label={i18n.common_clear_search()}
						class="-mr-1 grid h-6 w-6 place-items-center rounded-full text-accent-text"
					>
						<X size={12} aria-hidden="true" />
					</button>
				</span>
			{/if}
			{#each tabs as t (t.key)}
				<button
					type="button"
					onclick={() => onTabChange(t.key)}
					aria-current={t.key === tab ? "page" : undefined}
					class={cn(phoneChip, t.key === tab ? chipOn : chipOff)}
				>
					{#if t.dot}
						<span class={cn("h-1.5 w-1.5 rounded-full", t.dot)} aria-hidden="true"></span>
					{/if}
					{t.label}
					{#if t.count != null}
						<span class="font-mono text-[10.5px] tabular opacity-70">{t.count}</span>
					{/if}
				</button>
			{/each}
		</nav>
		<button
			type="button"
			onclick={() => (sheetOpen = true)}
			aria-haspopup="dialog"
			aria-expanded={sheetOpen}
			aria-label={i18n.filter_and_sort()}
			class={cn(
				"relative grid h-11 w-11 shrink-0 place-items-center rounded-lg border transition",
				activeCount > 0 ? chipOn : "border-border-strong bg-bg-elevated text-fg-muted",
			)}
		>
			<SlidersHorizontal size={16} aria-hidden="true" />
			{#if activeCount > 0}
				<span
					class="absolute -right-1 -top-1 grid h-4 min-w-4 place-items-center rounded-full bg-accent px-1 font-mono text-[9.5px] font-semibold text-fg-on-accent"
				>
					{activeCount}
				</span>
			{/if}
		</button>
	</div>
</div>

<!-- view="list" keeps the sheet's layout switch and selection entry out: the
     switch is md-only anyway, and these pages have one layout. -->
<MediaFilterSheet
	open={sheetOpen}
	onClose={() => (sheetOpen = false)}
	{query}
	{onQueryChange}
	sortOptions={sorts}
	{sort}
	{onSortChange}
	view="list"
	onViewChange={() => {}}
	{onReset}
	{activeCount}
	extra={facets.length > 0 ? facetChips : undefined}
/>
