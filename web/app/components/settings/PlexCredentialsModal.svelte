<script lang="ts">
	import { Check, Copy, TriangleAlert } from "@lucide/svelte";
	import { toast } from "../../lib/toast";
	import Modal from "../modals/Modal.svelte";

	type Props = {
		open: boolean;
		clientID: string;
		token: string;
		onClose: () => void;
	};
	let { open, clientID, token, onClose }: Props = $props();

	// `copied` holds the label of the last-copied field, for the checkmark swap.
	let copied = $state("");

	let snippet = $derived(
		`media_server:
  plex_client_id: ${clientID || "<client-id>"}
  servers:
    - name: plex
      server_type: plex
      host: http://plex.local:32400
      api_key: ${token || "<token>"}`,
	);

	let fields = $derived([
		...(clientID ? [{ label: "plex_client_id", value: clientID }] : []),
		...(token ? [{ label: "api_key", value: token }] : []),
	]);

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

<Modal {open} title="Connect Plex (read-only)" size="lg" onClose={close}>
	<div class="space-y-4 text-sm text-fg">
		<div
			class="flex items-start gap-2.5 rounded-md border border-status-wanted/40 bg-status-wanted/10 p-3 text-xs text-status-wanted"
		>
			<TriangleAlert size={14} class="mt-0.5 shrink-0" aria-hidden="true" />
			<p>
				This instance runs read-only, so Plex can't be saved from here. Finish
				sign-in in the Plex popup; the client ID + token appear below to add to
				your config and redeploy.
			</p>
		</div>

		{#each fields as f (f.label)}
			<div>
				<div class="mb-1 text-[11px] font-medium text-fg-muted">{f.label}</div>
				<div class="flex items-center gap-2">
					<code
						class="min-w-0 flex-1 truncate rounded-md border border-border bg-bg-base px-2.5 py-1.5 font-mono text-xs text-fg"
					>{f.value}</code>
					<button
						type="button"
						onclick={() => copy(f.value, f.label)}
						aria-label="Copy {f.label}"
						class="inline-flex shrink-0 items-center rounded-md border border-border p-2 text-fg-muted transition hover:bg-surface hover:text-fg"
					>
						{#if copied === f.label}
							<Check size={14} class="text-status-available" aria-hidden="true" />
						{:else}
							<Copy size={14} aria-hidden="true" />
						{/if}
					</button>
				</div>
			</div>
		{/each}

		{#if !token}
			<p class="text-xs text-fg-muted">
				Waiting for Plex sign-in — finish in the popup window…
			</p>
		{/if}

		{#if token}
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
					Edit <code class="font-mono">host</code> to your Plex address, then commit and redeploy.
				</p>
			</div>
		{/if}
	</div>

	{#snippet footer()}
		<button
			type="button"
			onclick={close}
			class="inline-flex h-9 items-center rounded-md border border-border px-3 text-sm font-medium text-fg-muted transition hover:bg-surface hover:text-fg"
		>
			Close
		</button>
	{/snippet}
</Modal>
