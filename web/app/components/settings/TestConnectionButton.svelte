<script lang="ts">
	import type { Snippet } from "svelte";
	import { createMutation } from "@tanstack/svelte-query";
	import { Plug, CircleCheck, CircleX, TriangleAlert } from "@lucide/svelte";
	import { api, errorText } from "../../lib/api";
	import { cn } from "../../lib/cn";
	import { toast } from "../../lib/toast";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	type Props = {
		endpoint: string;
		label?: string;
		size?: "sm" | "md";
		// "block" is the touch form's layout: a full-width button with the failure
		// reason rendered underneath it. "card" is a config card's footer: the
		// reason goes to a toast and only a verdict badge stays behind, sharing
		// the button's own row. A result panel under the button cost the card a
		// permanent empty reservation or a shove of the grid below it, and a badge
		// up among the card's status pills wrapped that row onto a second line —
		// the row is the one place in the card with spare width. The inline
		// variant puts the reason in a title attribute, which a touch device has
		// no way to show.
		variant?: "inline" | "block" | "card";
		// card only: the row's trailing controls (edit / delete). Passed in so the
		// component owns the whole footer.
		trailing?: Snippet;
		// When set, the connection test POSTs these values instead of hitting a
		// saved row — used by the create form to probe a draft. Read lazily so
		// the latest form state is sent at click time.
		body?: () => unknown;
	};

	let {
		endpoint,
		label = "Test connection",
		size = "sm",
		variant = "inline",
		trailing,
		body,
	}: Props = $props();

	const test = createMutation<null, Error, void>(() => ({
		mutationFn: () => api<null>(endpoint, { method: "POST", body: body?.() }),
		onSuccess: () => {
			if (variant === "card") toast.ok(i18n.test_connection_ok());
		},
		onError: (e) => {
			// The upstream status only ever appears in this message, so it is the
			// toast's body rather than a generic failure line.
			if (variant === "card") toast.err(errorText(e));
		},
	}));

	const sizing = $derived(
		size === "md" ? "h-9 px-3 text-sm" : "h-8 px-2.5 text-xs",
	);
</script>

{#if variant === "card"}
	<div class="mt-1 border-t border-border pt-3">
		<div class="flex items-center gap-2">
			<button
				type="button"
				disabled={test.isPending}
				onclick={() => test.mutate()}
				class="inline-flex h-9 min-w-0 flex-1 items-center justify-center gap-1.5 rounded-md border border-border bg-bg-base text-[13px] font-medium text-fg-muted transition hover:border-border-strong hover:text-fg active:bg-bg-hover disabled:cursor-progress disabled:opacity-60"
			>
				<Plug size={13} aria-hidden="true" />
				{test.isPending ? i18n.common_testing() : label}
			</button>
			{#if test.isSuccess || test.isError}
				<span
					class={cn(
						"inline-flex shrink-0 items-center gap-1 whitespace-nowrap rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide",
						test.isSuccess
							? "bg-status-available/10 text-status-available"
							: "bg-status-failed/10 text-status-failed",
					)}
					title={test.isError ? errorText(test.error) : undefined}
				>
					{#if test.isSuccess}
						<CircleCheck size={11} aria-hidden="true" />
						{i18n.conn_badge_ok()}
					{:else}
						<CircleX size={11} aria-hidden="true" />
						{i18n.conn_badge_failed()}
					{/if}
				</span>
			{/if}
			{@render trailing?.()}
		</div>
	</div>
{:else if variant === "block"}
	<div>
		<button
			type="button"
			disabled={test.isPending}
			onclick={() => test.mutate()}
			class="inline-flex h-11 w-full items-center justify-center gap-2 rounded-lg border border-border-strong text-sm font-medium text-fg-muted transition active:bg-bg-hover disabled:cursor-progress disabled:opacity-60"
		>
			<Plug size={15} aria-hidden="true" />
			{test.isPending ? i18n.common_testing() : label}
		</button>
		{#if test.isSuccess}
			<div
				class="mt-2.5 flex gap-2.5 rounded-lg border border-status-available/35 bg-status-available/[0.07] px-3 py-2.5 text-status-available"
			>
				<CircleCheck size={14} class="mt-0.5 shrink-0" aria-hidden="true" />
				<p class="text-[12.5px] font-medium">{i18n.test_connection_ok()}</p>
			</div>
		{:else if test.isError}
			<div
				class="mt-2.5 flex gap-2.5 rounded-lg border border-status-failed/35 bg-status-failed/[0.07] px-3 py-2.5 text-status-failed"
			>
				<TriangleAlert size={14} class="mt-0.5 shrink-0" aria-hidden="true" />
				<div class="min-w-0">
					<p class="text-[12.5px] font-semibold">
						{i18n.test_connection_failed()}
					</p>
					<p
						class="mt-1 break-words text-[11.5px] leading-relaxed text-status-failed/80"
					>
						{errorText(test.error)}
					</p>
				</div>
			</div>
		{/if}
	</div>
{:else}
	<div class="flex items-center gap-2">
	<button
		type="button"
		disabled={test.isPending}
		onclick={() => test.mutate()}
		class="inline-flex items-center gap-1.5 rounded-md border border-border bg-bg-base font-medium text-fg-muted transition hover:border-border-strong hover:text-fg disabled:cursor-progress disabled:opacity-60 {sizing}"
	>
		<Plug size={size === "md" ? 14 : 12} aria-hidden="true" />
		{test.isPending ? i18n.common_testing() : label}
	</button>

	{#if test.isSuccess}
		<span
			class="inline-flex items-center gap-1 text-xs font-medium text-status-available"
		>
			<CircleCheck size={12} aria-hidden="true" />
			{i18n.common_ok()}
		</span>
	{:else if test.isError}
		<span
			class="inline-flex items-center gap-1 text-xs font-medium text-status-failed"
			title={errorText(test.error)}
		>
			<CircleX size={12} aria-hidden="true" />
			{i18n.status_failed()}
		</span>
	{/if}
</div>
{/if}
