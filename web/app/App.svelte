<script lang="ts">
	import { Router } from "@roxi/routify";
	import { QueryClientProvider } from "@tanstack/svelte-query";
	import { Toaster } from "svelte-sonner";
	import { routes } from "./.routify/routes.default.js";
	import { queryClient } from "@lib/query.js";
	import { createMediaQuery } from "@lib/viewport.svelte";
	import GlobalActivityBar from "./components/layout/GlobalActivityBar.svelte";
	import QueueWatcher from "./components/layout/QueueWatcher.svelte";

	// Below md the bottom of the screen is where the thumb actions live — the
	// BottomNav and every modal/sheet footer ("Open in library" right after an
	// add) — and a bottom toast lands on top of them. Phones get the toasts at
	// the top instead, clear of the status bar on a notched PWA.
	const isDesktop = createMediaQuery("(min-width: 768px)");
	const safeTop = "calc(env(safe-area-inset-top) + 16px)";
</script>

<QueryClientProvider client={queryClient}>
	<GlobalActivityBar />
	<QueueWatcher />
	<Router {routes} />
	<Toaster
		richColors
		closeButton
		position={isDesktop() ? "bottom-right" : "top-center"}
		offset={isDesktop() ? undefined : { top: safeTop }}
		mobileOffset={{ top: safeTop }}
		theme="dark"
	/>
</QueryClientProvider>
