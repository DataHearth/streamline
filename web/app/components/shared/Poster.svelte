<script lang="ts">
	import { onDestroy } from "svelte";
	import Img from "@components/shared/Img.svelte";
	import { isPosterMissing, markPosterMissing, POSTER_SIZES, posterSrcset } from "@lib/posters";

	let {
		src,
		alt,
		class: klass = "",
		loading = "lazy",
		sizes,
		...rest
	}: {
		src: string;
		alt: string;
		class?: string;
		loading?: "eager" | "lazy";
		// The slot's width, for a poster loaded eagerly: only a lazy one has it
		// read from layout.
		sizes?: string;
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

	const retry = $derived(attempt === 0 ? "" : `?retry=${attempt}`);
	const url = $derived(src + retry);
	// Without a slot width the browser assumes the whole viewport and fetches
	// the full-size poster, so an eager poster with no `sizes` keeps `src` alone.
	const slot = $derived(sizes ?? (loading === "lazy" ? POSTER_SIZES : undefined));
	const srcset = $derived(slot ? posterSrcset(src, retry) : undefined);

	function handleError() {
		// The lookup proxy resolves on the request itself, so its 404 already
		// means "no art"; retrying only fills the console.
		if (attempt >= retryDelays.length || src.startsWith("/posters/lookup/")) {
			markPosterMissing(src);
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

{#if visible && !isPosterMissing(src)}
	<!-- `visible` flips false on every failure, so each attempt mounts a fresh
	`Img` whose reveal state starts over — a retry shown before it has decoded is
	exactly what a persisted one would cause. -->
	<Img src={url} {srcset} sizes={srcset ? slot : undefined} {alt} class={klass} {loading} onerror={handleError} {...rest} />
{/if}
