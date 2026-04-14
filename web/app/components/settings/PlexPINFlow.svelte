<script lang="ts">
	import { Cast, CircleCheck, RefreshCw } from "@lucide/svelte";
	import { toast } from "../../lib/toast";
	import { startPlexPin } from "../../lib/plex_pin";
	import PlexCredentialsModal from "./PlexCredentialsModal.svelte";

	type Props = {
		token: string;
		onToken: (token: string, clientID: string) => void;
		// onClientId fires as soon as the flow starts (before sign-in completes),
		// so callers can surface the client id immediately.
		onClientId?: (clientID: string) => void;
	};

	let { token, onToken, onClientId }: Props = $props();

	let busy = $state(false);
	// The modal opens on connect and shows the client id right away, then the
	// token once Plex sign-in completes — for copying both into config.
	let credsOpen = $state(false);
	let credsClientID = $state("");
	let credsToken = $state("");

	function start() {
		busy = true;
		credsClientID = "";
		credsToken = "";
		credsOpen = true;
		startPlexPin({
			onClientId: (cid) => {
				credsClientID = cid;
				onClientId?.(cid);
			},
			onToken: (t, cid) => {
				credsToken = t;
				onToken(t, cid);
				toast.ok(
					"Plex connected — you can now Test the connection or Discover sections.",
				);
			},
			onDone: () => (busy = false),
		});
	}
</script>

<div
	class="flex flex-wrap items-center gap-3 rounded-md border border-border bg-bg-base px-3 py-2"
>
	<button
		type="button"
		disabled={busy}
		onclick={start}
		class="inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md bg-accent px-3 py-1.5 text-xs font-semibold text-fg-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-50"
	>
		{#if token}
			<RefreshCw size={14} aria-hidden="true" />
			Reconnect
		{:else}
			<Cast size={14} aria-hidden="true" />
			Connect with Plex
		{/if}
	</button>
	<span class="min-w-0 flex-1 text-[11px] text-fg-muted">
		PIN sign-in opens in a popup; the token fills in here.
	</span>
	{#if token}
		<span
			class="inline-flex items-center gap-1.5 text-xs text-status-available"
		>
			<CircleCheck size={12} aria-hidden="true" />
			Token connected
		</span>
	{/if}
</div>

<PlexCredentialsModal
	open={credsOpen}
	clientID={credsClientID}
	token={credsToken}
	onClose={() => (credsOpen = false)}
/>
