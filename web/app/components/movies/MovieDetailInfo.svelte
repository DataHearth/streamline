<script lang="ts">
	import { ExternalLink } from "@lucide/svelte";
	import type { Movie, MediaFile } from "../../lib/types";

	let {
		movie,
		qualityProfileName,
	}: {
		movie: Movie;
		qualityProfileName: string | null;
	} = $props();

	function pickPrimary(
		files: MediaFile[] | undefined,
	): MediaFile | undefined {
		if (!files || files.length === 0) return undefined;
		return [...files].sort((a, b) => b.size - a.size)[0];
	}

	function totalSize(files: MediaFile[] | undefined): string {
		if (!files || files.length === 0) return "—";
		const bytes = files.reduce((s, f) => s + f.size, 0);
		const gb = bytes / 1_073_741_824;
		if (gb >= 1) return `${gb.toFixed(1)} GB`;
		const mb = bytes / 1_048_576;
		return `${mb.toFixed(0)} MB`;
	}

	let primary = $derived(pickPrimary(movie.media_files));
</script>

<aside class="flex flex-col gap-4">
	<section
		class="rounded-lg border border-border bg-bg-elevated p-5"
		aria-labelledby="info-file"
	>
		<h4
			id="info-file"
			class="font-mono text-[11px] uppercase tracking-[0.14em] text-fg-faint"
		>
			File
		</h4>
		{#if primary}
			<dl class="mt-3 grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-[12px]">
				{#if primary.parsed_resolution}
					<dt class="text-fg-subtle">Resolution</dt>
					<dd class="text-right font-mono text-fg">
						{primary.parsed_resolution}
					</dd>
				{/if}
				{#if primary.parsed_codec}
					<dt class="text-fg-subtle">Codec</dt>
					<dd class="text-right font-mono text-fg">
						{primary.parsed_codec}
					</dd>
				{/if}
				{#if primary.parsed_source}
					<dt class="text-fg-subtle">Source</dt>
					<dd class="text-right font-mono text-fg">
						{primary.parsed_source}
					</dd>
				{/if}
				<dt class="text-fg-subtle">Size</dt>
				<dd class="text-right font-mono text-fg">
					{totalSize(movie.media_files)}
				</dd>
				{#if primary.release_group}
					<dt class="text-fg-subtle">Group</dt>
					<dd class="text-right font-mono text-fg">
						{primary.release_group}
					</dd>
				{/if}
			</dl>
		{:else}
			<p class="mt-3 text-[12px] text-fg-subtle">
				No file yet.{movie.status === "wanted"
					? " Searching nightly."
					: ""}
			</p>
		{/if}
	</section>

	<section
		class="rounded-lg border border-border bg-bg-elevated p-5"
		aria-labelledby="info-library"
	>
		<h4
			id="info-library"
			class="font-mono text-[11px] uppercase tracking-[0.14em] text-fg-faint"
		>
			Library
		</h4>
		<dl class="mt-3 grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-[12px]">
			<dt class="text-fg-subtle">Quality profile</dt>
			<dd class="text-right font-mono text-fg">
				{qualityProfileName ?? "—"}
			</dd>
			<dt class="text-fg-subtle">Status</dt>
			<dd class="text-right font-mono text-fg capitalize">
				{movie.status}
			</dd>
			<dt class="text-fg-subtle">Monitored</dt>
			<dd class="text-right font-mono text-fg">
				{movie.monitored ? "Yes" : "No"}
			</dd>
			{#if movie.year}
				<dt class="text-fg-subtle">Year</dt>
				<dd class="text-right font-mono text-fg">{movie.year}</dd>
			{/if}
			{#if movie.runtime}
				<dt class="text-fg-subtle">Runtime</dt>
				<dd class="text-right font-mono text-fg">{movie.runtime}m</dd>
			{/if}
			<dt class="text-fg-subtle">TMDB</dt>
			<dd class="text-right">
				<a
					href="https://www.themoviedb.org/movie/{movie.tmdb_id}"
					target="_blank"
					rel="noopener noreferrer"
					class="inline-flex items-center gap-1 font-mono text-accent-text transition hover:text-accent"
				>
					{movie.tmdb_id}
					<ExternalLink size={11} aria-hidden="true" />
				</a>
			</dd>
		</dl>
	</section>
</aside>
