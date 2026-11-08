<script lang="ts">
	import { Router } from "@roxi/routify";
	import { QueryClientProvider } from "@tanstack/svelte-query";
	import { Toaster } from "svelte-sonner";
	import { routes } from "./.routify/routes.default.js";
	import { queryClient } from "@lib/query.js";
	import GlobalActivityBar from "./components/layout/GlobalActivityBar.svelte";
	import QueueWatcher from "./components/layout/QueueWatcher.svelte";
</script>

<QueryClientProvider client={queryClient}>
	<GlobalActivityBar />
	<QueueWatcher />
	<Router {routes} />
	<!-- Sonner derives right + bottom only from bottom-right. On a phone the
	     toast spans the screen and the natural flick is horizontal either way,
	     as on an Android notification, so left dismisses too. -->
	<Toaster
		richColors
		closeButton
		position="bottom-right"
		theme="dark"
		swipeDirections={["left", "right", "bottom"]}
	/>
</QueryClientProvider>
