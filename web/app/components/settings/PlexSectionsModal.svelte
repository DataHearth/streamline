<script lang="ts">
	import { Check, Copy, TriangleAlert } from "@lucide/svelte";
	import { toast } from "../../lib/toast";
	import Modal from "../modals/Modal.svelte";
	import type { MediaServerSection } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	type Props = {
		open: boolean;
		serverName: string;
		sections: MediaServerSection[];
		// The keys currently picked in the form's section dropdowns. A library
		// can hold several sections of one type (a streamline one beside an
		// existing Radarr/Sonarr one), so which is "yours" is a choice only the
		// operator can make — the snippet follows it rather than guessing.
		selectedMovie: string;
		selectedShow: string;
		onClose: () => void;
	};
	let {
		open,
		serverName,
		sections,
		selectedMovie,
		selectedShow,
		onClose,
	}: Props = $props();

	// `copied` holds the label of the last-copied field, for the checkmark swap.
	let copied = $state("");

	let movies = $derived(sections.filter((s) => s.type === "movie"));
	let shows = $derived(sections.filter((s) => s.type === "show"));

	let movieKey = $derived(selectedMovie || movies[0]?.key || "<movie-section-key>");
	let showKey = $derived(selectedShow || shows[0]?.key || "<tv-section-key>");

	let snippet = $derived(
		`media_server:
  servers:
    - name: ${serverName || "plex"}
      server_type: plex
      library_section: ${movieKey}
      library_section_tv: ${showKey}`,
	);

	async function copy(value: string, label: string) {
		try {
			await navigator.clipboard.writeText(value);
			copied = label;
			toast.ok("Copied");
			setTimeout(() => {
				if (copied === label) copied = "";
			}, 1500);
		} catch {
			toast.err("Clipboard unavailable");
		}
	}

	function close() {
		copied = "";
		onClose();
	}
</script>

<Modal {open} title={i18n.mediaserver_plex_sections()} size="lg" onClose={close}>
	<div class="space-y-4 text-sm text-fg">
		<div
			class="flex items-start gap-2.5 rounded-md border border-status-wanted/40 bg-status-wanted/10 p-3 text-xs text-status-wanted"
		>
			<TriangleAlert size={14} class="mt-0.5 shrink-0" aria-hidden="true" />
			<p>
				{i18n.plex_sections_readonly_help()}
			</p>
		</div>

		{#each [{ heading: i18n.mediaserver_library_section(), key: "library_section", list: movies }, { heading: i18n.mediaserver_library_section_tv(), key: "library_section_tv", list: shows }] as group (group.key)}
			<div>
				<div class="mb-1 text-[11px] font-medium text-fg-muted">
					{group.key}
				</div>
				{#if group.list.length === 0}
					<p class="text-xs text-fg-faint">{i18n.plex_sections_none()}</p>
				{:else}
					<div class="space-y-2">
						{#each group.list as s (s.key)}
							<div class="flex items-center gap-2">
								<code
									class="min-w-0 flex-1 truncate rounded-md border border-border bg-bg-base px-2.5 py-1.5 font-mono text-xs text-fg"
								>{s.key} · {s.name}{s.locations.length ? ` — ${s.locations.join(", ")}` : ""}</code>
								<button
									type="button"
									onclick={() => copy(s.key, `${group.key}:${s.key}`)}
									aria-label="Copy {s.name} section key"
									class="inline-flex shrink-0 items-center rounded-md border border-border p-2 text-fg-muted transition hover:bg-surface hover:text-fg"
								>
									{#if copied === `${group.key}:${s.key}`}
										<Check size={14} class="text-status-available" aria-hidden="true" />
									{:else}
										<Copy size={14} aria-hidden="true" />
									{/if}
								</button>
							</div>
						{/each}
					</div>
				{/if}
			</div>
		{/each}

		<div>
			<div class="mb-1 flex items-center justify-between">
				<span class="text-[11px] font-medium text-fg-muted">config.yaml</span>
				<button
					type="button"
					onclick={() => copy(snippet, "yaml")}
					class="inline-flex items-center gap-1.5 rounded-md border border-border px-2 py-1 text-[11px] font-medium text-fg-muted transition hover:bg-surface hover:text-fg"
				>
					{#if copied === "yaml"}
						<Check size={12} class="text-status-available" aria-hidden="true" />
						Copied
					{:else}
						<Copy size={12} aria-hidden="true" />
						Copy YAML
					{/if}
				</button>
			</div>
			<pre
				class="overflow-x-auto rounded-md border border-border bg-bg-base p-3 font-mono text-[11px] leading-relaxed text-fg">{snippet}</pre>
			<p class="mt-1 text-[11px] text-fg-muted">
				{i18n.plex_sections_commit_help()}
			</p>
		</div>
	</div>

	{#snippet footer()}
		<button
			type="button"
			onclick={close}
			class="inline-flex h-9 items-center rounded-md border border-border px-3 text-sm font-medium text-fg-muted transition hover:bg-surface hover:text-fg"
		>
			{i18n.common_close()}
		</button>
	{/snippet}
</Modal>
