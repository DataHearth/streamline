<script lang="ts">
	import { createMutation } from "@tanstack/svelte-query";
	import { Plug, CircleCheck, CircleX } from "@lucide/svelte";
	import { api } from "../../lib/api";

	type Props = {
		endpoint: string;
		label?: string;
		size?: "sm" | "md";
		// When set, the connection test POSTs these values instead of hitting a
		// saved row — used by the create form to probe a draft. Read lazily so
		// the latest form state is sent at click time.
		body?: () => unknown;
	};

	let { endpoint, label = "Test connection", size = "sm", body }: Props =
		$props();

	const test = createMutation<null, Error, void>(() => ({
		mutationFn: () => api<null>(endpoint, { method: "POST", body: body?.() }),
	}));

	const sizing = $derived(
		size === "md" ? "h-9 px-3 text-sm" : "h-8 px-2.5 text-xs",
	);
</script>

<div class="flex items-center gap-2">
	<button
		type="button"
		disabled={test.isPending}
		onclick={() => test.mutate()}
		class="inline-flex items-center gap-1.5 rounded-md border border-border bg-bg-base font-medium text-fg-muted transition hover:border-border-strong hover:text-fg disabled:cursor-progress disabled:opacity-60 {sizing}"
	>
		<Plug size={size === "md" ? 14 : 12} aria-hidden="true" />
		{test.isPending ? "Testing…" : label}
	</button>

	{#if test.isSuccess}
		<span
			class="inline-flex items-center gap-1 text-xs font-medium text-status-available"
		>
			<CircleCheck size={12} aria-hidden="true" />
			OK
		</span>
	{:else if test.isError}
		<span
			class="inline-flex items-center gap-1 text-xs font-medium text-status-failed"
			title={test.error?.message}
		>
			<CircleX size={12} aria-hidden="true" />
			Failed
		</span>
	{/if}
</div>
