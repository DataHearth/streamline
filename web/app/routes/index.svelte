<script lang="ts">
	import { onMount } from "svelte";

	onMount(() => {
		window.location.replace("/dashboard");
	});
</script>

<p class="p-6 text-fg-muted">Redirecting to dashboard…</p>
