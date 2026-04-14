<script lang="ts">
	import { Router } from "@roxi/routify";
	import { QueryClientProvider } from "@tanstack/svelte-query";
	import { Toaster } from "svelte-sonner";
	import { routes } from "./.routify/routes.default.js";
	import { queryClient } from "./lib/query.js";
</script>

<QueryClientProvider client={queryClient}>
	<Router {routes} />
	<Toaster richColors closeButton position="bottom-right" theme="dark" />
</QueryClientProvider>
