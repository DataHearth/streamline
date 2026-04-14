<script lang="ts">
	import { createMutation } from "@tanstack/svelte-query";
	import { ShieldAlert, RotateCcw } from "@lucide/svelte";
	import { api } from "../../lib/api";
	import { auth } from "../../lib/auth.svelte";
	import { toast } from "../../lib/toast";
	import Dialog from "../modals/Dialog.svelte";

	const rotate = createMutation<{ token: string }, Error, void>(() => ({
		mutationFn: () =>
			api<{ token: string }>("/auth/jwt/rotate", { method: "POST" }),
		onSuccess: () => {
			toast.ok("JWT secret rotated — signing out");
			window.location.href = "/login";
		},
		onError: (err) => {
			toast.err(err.message);
			open = false;
		},
	}));

	let open = $state(false);
	let typed = $state("");
	// Reset the type-to-confirm input each time the dialog opens.
	$effect(() => {
		if (open) typed = "";
	});
	let canRotate = $derived(typed === "rotate" && !rotate.isPending);

	let isAdmin = $derived(auth.user?.role === "admin");
</script>

{#if isAdmin}
	<section
		class="overflow-hidden rounded-lg border border-status-failed/40 bg-status-failed/5"
	>
		<div
			class="flex flex-col gap-4 p-6 md:flex-row md:items-center md:justify-between"
		>
			<div class="flex items-start gap-3.5">
				<span
					class="grid h-10 w-10 shrink-0 place-items-center rounded-md bg-status-failed/12 text-status-failed"
					aria-hidden="true"
				>
					<ShieldAlert size={20} />
				</span>
				<div>
					<h3 class="text-base font-semibold text-fg">
						Rotate JWT signing key
					</h3>
					<p class="mt-1 max-w-xl text-sm text-fg-muted">
						Generates a fresh HMAC signing secret and invalidates
						every active session and API key, including your own.
						Use after a suspected secret compromise — everyone has
						to sign back in.
					</p>
				</div>
			</div>
			<button
				type="button"
				disabled={rotate.isPending}
				onclick={() => (open = true)}
				class="inline-flex h-10 shrink-0 items-center gap-2 rounded-md border border-status-failed/40 bg-status-failed/10 px-4 text-sm font-semibold text-status-failed transition hover:bg-status-failed/20 disabled:cursor-not-allowed disabled:opacity-60"
			>
				<RotateCcw size={14} aria-hidden="true" />
				{rotate.isPending ? "Rotating…" : "Rotate key…"}
			</button>
		</div>
	</section>

	<Dialog
		{open}
		title="Rotate the JWT signing key?"
		onClose={() => {
			if (!rotate.isPending) open = false;
		}}
		actions={[
			{ label: "Cancel", variant: "ghost" },
			{
				label: "Rotate key",
				variant: "danger",
				dismiss: false,
				disabled: !canRotate,
				pending: rotate.isPending,
				onClick: () => rotate.mutate(),
			},
		]}
	>
		<p class="text-sm leading-relaxed text-fg-muted">
			A fresh HMAC secret will be generated, persisted to config, and every
			active session and API key will be invalidated. You will be signed
			out immediately.
		</p>
		<label class="mt-4 block">
			<span class="mb-1 block text-xs font-medium text-fg-muted">
				Type
				<code class="rounded bg-bg-deep px-1 py-0.5 font-mono text-fg">
					rotate
				</code>
				to confirm
			</span>
			<input
				type="text"
				bind:value={typed}
				autocomplete="off"
				spellcheck="false"
				class="h-9 w-full rounded-md border border-border bg-bg px-3 font-mono text-sm text-fg placeholder:text-fg-faint focus-visible:outline-2 focus-visible:outline-accent"
			/>
		</label>
	</Dialog>
{/if}
