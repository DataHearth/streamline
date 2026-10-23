<script lang="ts">
	import { NOUN_TITLE, type Noun } from "@lib/nouns";
	import { selectionHeader } from "@lib/selection-header.svelte";

	// Phone selection header. It draws nothing here: while mounted it hands the
	// selection to the top bar, which shows close, the count and select all in
	// place of the page title (lib/selection-header). Pages mount it wherever a
	// selection is live; the count is the title, so the bar can never claim a
	// set it doesn't have.
	let {
		count,
		total,
		noun = NOUN_TITLE,
		onClear,
		onSelectAll,
	}: {
		count: number;
		total: number;
		noun?: Noun;
		onClear: () => void;
		onSelectAll: () => void;
	} = $props();

	$effect(() =>
		selectionHeader.claim(() => ({ count, total, noun, onClear, onSelectAll })),
	);
</script>
