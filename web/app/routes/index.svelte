<script lang="ts">
	import { onMount } from "svelte";
	import { m as i18n } from "../lib/paraglide/messages.js";

	onMount(() => {
		window.location.replace("/dashboard");
	});
</script>

<p class="p-6 text-fg-muted">{i18n.root_redirecting()}</p>
