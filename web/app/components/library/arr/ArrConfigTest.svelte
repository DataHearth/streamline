<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { Plug } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import ArrPill from "./ArrPill.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// A connection test for an indexer or download client the wizard has just
	// added. The verdict stays on the row — a toast would vanish before the
	// operator could match it to one of several names.
	type Props = {
		endpoint: string;
		queryKey: string;
	};

	let { endpoint, queryKey }: Props = $props();

	const qc = useQueryClient();

	const test = createMutation<null, Error, void>(() => ({
		mutationFn: () => api<null>(endpoint, { method: "POST" }),
		onSettled: () => qc.invalidateQueries({ queryKey: [queryKey] }),
	}));
</script>

<div class="flex min-w-0 flex-wrap items-center gap-2" aria-live="polite">
	<button
		type="button"
		disabled={test.isPending}
		onclick={() => test.mutate()}
		class="inline-flex h-8 shrink-0 items-center gap-1.5 rounded-md border border-border bg-bg-base px-2.5 text-xs font-medium text-fg-muted transition hover:border-border-strong hover:text-fg disabled:cursor-progress disabled:opacity-60"
	>
		<Plug size={13} aria-hidden="true" />
		{test.isPending ? i18n.common_testing() : i18n.arr_config_test()}
	</button>
	{#if test.isSuccess}
		<ArrPill tone="ok" label={i18n.arr_config_test_ok()} />
	{:else if test.isError}
		<p role="alert" class="min-w-0 basis-full text-xs break-words text-status-failed">
			{errorText(test.error)}
		</p>
	{/if}
</div>
