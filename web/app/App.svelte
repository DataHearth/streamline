<script lang="ts">
	import { Router } from "@roxi/routify";
	import { QueryClientProvider } from "@tanstack/svelte-query";
	import { Toaster } from "svelte-sonner";
	import { routes } from "./.routify/routes.default.js";
	import { queryClient } from "./lib/query.js";
	import GlobalActivityBar from "./components/layout/GlobalActivityBar.svelte";
	import QueueWatcher from "./components/layout/QueueWatcher.svelte";
</script>

<QueryClientProvider client={queryClient}>
	<GlobalActivityBar />
	<QueueWatcher />
	<Router {routes} />
	<Toaster richColors closeButton position="bottom-right" theme="dark" />
</QueryClientProvider>
