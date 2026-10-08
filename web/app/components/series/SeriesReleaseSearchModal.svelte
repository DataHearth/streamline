<script lang="ts">
	import { around } from "@lib/message-parts";
	import { Info } from "@lucide/svelte";
	import Modal from "@components/modals/Modal.svelte";
	import ReleasesTable from "@components/shared/ReleasesTable.svelte";
	import Select from "@components/forms/Select.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	const [packsPre, packsPost] = around((scope) =>
		i18n.series_packs_hidden_switch({ scope }),
	);

	let {
		open,
		seriesId,
		seasons,
		initialScope = "series",
		scopeLabel,
		fileCounts = {},
		onClose,
	}: {
		open: boolean;
		seriesId: number;
		// e.g. "Breaking Bad (2008)"; shown in the modal title for context.
		scopeLabel?: string;
		// Season numbers that have episodes, ascending. 0 = Specials. "series"
		// scope searches the whole show (integral / multi-season packs).
		seasons: { number: number; label: string }[];
		// Scope the modal opens on, so a per-season entry point lands on that
		// season instead of making the operator re-pick what they just clicked.
		initialScope?: string;
		// Episode files already on disk per scope ("series" or a season number as a
		// string). Above zero, picking a pack asks before replacing them.
		fileCounts?: Record<string, number>;
		onClose: () => void;
	} = $props();

	// scope is "series" (whole show) or a season number as a string.
	let scope = $state("series");
	// Reset to defaults each time the modal reopens.
	$effect(() => {
		if (open) scope = initialScope;
	});

	let options = $derived([
		{ value: "series", label: i18n.series_whole_series() },
		...seasons.map((s) => ({ value: String(s.number), label: s.label })),
	]);

	let searchPath = $derived(
		scope === "series"
			? `/series/${seriesId}/browse`
			: `/series/${seriesId}/seasons/${scope}/search`,
	);
	let grabPath = $derived(
		scope === "series"
			? `/series/${seriesId}/grab`
			: `/series/${seriesId}/seasons/${scope}/grab`,
	);
	let queryKey = $derived<readonly unknown[]>(
		scope === "series"
			? ["releases", "series", seriesId]
			: ["releases", "season", seriesId, scope],
	);
</script>

<Modal
	{open}
	title={scopeLabel
		? i18n.manual_search_scope({ scope: scopeLabel })
		: i18n.action_manual_search()}
	size="4xl"
	{onClose}
>
	<div class="mb-4 flex flex-wrap items-center gap-3">
		<span class="text-xs font-medium uppercase tracking-wide text-fg-subtle">
			{i18n.series_scope()}
		</span>
		<div class="w-56">
			<Select
				value={scope}
				{options}
				ariaLabel={i18n.series_search_scope()}
				onChange={(v) => (scope = v)}
			/>
		</div>
	</div>
	{#if scope !== "series"}
		<p class="mb-4 -mt-1 flex items-start gap-1.5 text-xs text-fg-subtle">
			<Info size={13} class="mt-px shrink-0" aria-hidden="true" />
			<span>
				{packsPre}<span class="font-medium text-fg-muted">{i18n.series_whole_series()}</span
				>{packsPost}
			</span>
		</p>
	{/if}
	{#key searchPath}
		<ReleasesTable
			{searchPath}
			{grabPath}
			{queryKey}
			existingCount={fileCounts[scope] ?? 0}
			enabled={open}
			onGrabbed={onClose}
		/>
	{/key}
</Modal>
