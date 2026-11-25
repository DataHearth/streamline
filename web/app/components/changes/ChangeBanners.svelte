<script lang="ts">
	import { twMerge } from "tailwind-merge";
	import { changesFor, pendingChangesQuery } from "@lib/metadata-changes";
	import type { MetadataChangeOwnerType } from "@lib/types";
	import ChangeCard from "./ChangeCard.svelte";

	// The pending provider changes of one item, as banners on its own page.
	// Admins only: the list is not fetched for anyone else.
	let {
		type,
		id,
		onPickEdition,
		class: klass,
	}: {
		type: MetadataChangeOwnerType;
		id: number;
		onPickEdition?: () => void;
		class?: string;
	} = $props();

	const changes = pendingChangesQuery();
	const mine = $derived(changesFor(changes.data?.items, type, id));
</script>

{#if mine.length > 0}
	<div class={twMerge("flex flex-col gap-3", klass)}>
		{#each mine as c (c.id)}
			<ChangeCard change={c} variant="banner" {onPickEdition} />
		{/each}
	</div>
{/if}
