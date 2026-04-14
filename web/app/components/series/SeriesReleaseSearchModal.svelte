<script lang="ts">
	import { Info } from "@lucide/svelte";
	import Modal from "../modals/Modal.svelte";
	import ReleasesTable from "../movies/ReleasesTable.svelte";
	import ReplaceExistingToggle from "../movies/ReplaceExistingToggle.svelte";
	import Select from "../forms/Select.svelte";

	let {
		open,
		seriesId,
		seasons,
		onClose,
	}: {
		open: boolean;
		seriesId: number;
		// Season numbers that have episodes, ascending. 0 = Specials. "series"
		// scope searches the whole show (integral / multi-season packs).
		seasons: { number: number; label: string }[];
		onClose: () => void;
	} = $props();

	// scope is "series" (whole show) or a season number as a string.
	let scope = $state("series");
	let replaceExisting = $state(false);
	// Reset to defaults each time the modal reopens.
	$effect(() => {
		if (open) {
			scope = "series";
			replaceExisting = false;
		}
	});

	let options = $derived([
		{ value: "series", label: "Whole series" },
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

<Modal {open} title="Manual search" size="4xl" {onClose}>
	<div class="mb-4 flex flex-wrap items-center gap-3">
		<span class="text-xs font-medium uppercase tracking-wide text-fg-subtle">
			Scope
		</span>
		<div class="w-56">
			<Select
				value={scope}
				{options}
				ariaLabel="Search scope"
				onChange={(v) => (scope = v)}
			/>
		</div>
		<div class="ml-auto">
			<ReplaceExistingToggle
				checked={replaceExisting}
				onChange={(v) => (replaceExisting = v)}
			/>
		</div>
	</div>
	{#if scope !== "series"}
		<p class="mb-4 -mt-1 flex items-start gap-1.5 text-xs text-fg-subtle">
			<Info size={13} class="mt-px shrink-0" aria-hidden="true" />
			<span>
				Complete and multi-season packs are hidden in a season search —
				switch to <span class="font-medium text-fg-muted">Whole series</span>
				to grab those.
			</span>
		</p>
	{/if}
	{#key searchPath}
		<ReleasesTable
			{searchPath}
			{grabPath}
			{queryKey}
			{replaceExisting}
			enabled={open}
			onGrabbed={onClose}
		/>
	{/key}
</Modal>
