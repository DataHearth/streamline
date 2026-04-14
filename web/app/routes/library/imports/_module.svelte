<script lang="ts">
	import { auth } from "../../../lib/auth.svelte";
	import { requireAdmin } from "../../../lib/guards";

	// Imports is admin-only; non-admins who reach the URL directly get
	// redirected to /forbidden once auth resolves. Mirrors settings/_module.
	$effect(() => {
		if (!auth.loading) requireAdmin();
	});
</script>

<!-- svelte-ignore slot_element_deprecated -->
<slot />
