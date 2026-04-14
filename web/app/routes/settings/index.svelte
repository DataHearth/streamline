<script lang="ts">
	import { onMount } from "svelte";
	import { goto } from "@roxi/routify";
	import { get } from "svelte/store";

	onMount(() => {
		get(goto)("/settings/general");
	});
</script>
