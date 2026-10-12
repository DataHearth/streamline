<script lang="ts">
	import { Send } from "@lucide/svelte";
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { api, errorText } from "@lib/api";
	import { auth } from "@lib/auth.svelte";
	import { INPUT_CLASS } from "@lib/form";
	import { toast } from "@lib/toast";
	import type { User } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	const qc = useQueryClient();

	let saved = $derived(auth.user?.device_email ?? "");
	let draft = $state<string | null>(null);
	let value = $derived(draft ?? saved);
	let send = $derived(auth.user?.send);

	const save = createMutation<User, Error, string>(() => ({
		mutationFn: (device_email) => api<User>("/auth/me", { method: "PATCH", body: { device_email } }),
		onSuccess: (user) => {
			auth.user = user;
			draft = null;
			qc.invalidateQueries({ queryKey: ["auth", "me"] });
			toast.ok(i18n.account_device_saved());
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	function commit() {
		if (draft === null) return;
		const next = draft.trim();
		if (next === saved) {
			draft = null;
			return;
		}
		save.mutate(next);
	}
</script>

<section class="overflow-hidden rounded-lg border border-border bg-bg-elevated">
	<header class="flex items-start gap-3 border-b border-border px-5 py-3.5">
		<div class="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-bg-card text-fg-muted" aria-hidden="true">
			<Send size={18} />
		</div>
		<div class="min-w-0 flex-1">
			<h3 class="text-base font-semibold text-fg">{i18n.account_device_title()}</h3>
			<p class="mt-0.5 text-xs text-fg-muted">{i18n.account_device_help()}</p>
		</div>
	</header>
	<div class="grid gap-2 px-5 py-4">
		<label class="block">
			<span class="mb-1 block text-sm font-medium text-fg">{i18n.account_device_email()}</span>
			<input
				type="email"
				autocomplete="off"
				class={INPUT_CLASS}
				placeholder={i18n.account_device_placeholder()}
				{value}
				disabled={save.isPending}
				oninput={(e) => (draft = e.currentTarget.value)}
				onblur={commit}
				onkeydown={(e) => {
					if (e.key === "Enter") e.currentTarget.blur();
				}}
			/>
		</label>
		{#if send?.configured}
			<p class="text-xs text-fg-muted">{i18n.account_device_approve({ from: send.from })}</p>
		{:else}
			<p class="text-xs text-fg-muted">{i18n.account_device_unconfigured()}</p>
		{/if}
	</div>
</section>
