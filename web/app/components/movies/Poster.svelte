<script lang="ts">
	import { onDestroy } from "svelte";

	let {
		src,
		alt,
		class: klass = "",
		loading = "lazy",
		...rest
	}: {
		src: string;
		alt: string;
		class?: string;
		loading?: "eager" | "lazy";
		[key: string]: unknown;
	} = $props();

	// Posters are fetched async server-side after a movie is added, so the
	// first few requests can 404. Browsers negative-cache 404s, so retries
	// need a unique URL — `?retry=N` busts the cache without changing how
	// the file is served.
	const retryDelays = [1000, 2000, 4000, 8000, 16000];
	let attempt = $state(0);
	let visible = $state(true);
	let timer: ReturnType<typeof setTimeout> | undefined;

	const url = $derived(attempt === 0 ? src : `${src}?retry=${attempt}`);

	function handleError() {
		if (attempt >= retryDelays.length) {
			visible = false;
			return;
		}
		visible = false;
		timer = setTimeout(() => {
			attempt += 1;
			visible = true;
		}, retryDelays[attempt]);
	}

	onDestroy(() => clearTimeout(timer));
</script>

{#if visible}
	<img
		src={url}
		{alt}
		class={klass}
		{loading}
		onerror={handleError}
		{...rest}
	/>
{/if}
