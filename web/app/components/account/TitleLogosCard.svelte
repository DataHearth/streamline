<script lang="ts">
	import { api, errorText } from "@lib/api";
	import { auth } from "@lib/auth.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";
	import { toast } from "@lib/toast";
	import type { User } from "@lib/types";
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { twMerge } from "tailwind-merge";

	const qc = useQueryClient();

	const save = createMutation<User, Error, boolean>(() => ({
		mutationFn: (title_logos) =>
			api<User>("/auth/me", { method: "PATCH", body: { title_logos } }),
		onSuccess: (user) => {
			auth.user = user;
			qc.invalidateQueries({ queryKey: ["auth", "me"] });
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	// The pressed button follows the click at once; a failed save falls back
	// to the stored value.
	let current = $derived(
		save.isPending ? save.variables : (auth.user?.title_logos ?? true),
	);

	const options = [
		{ on: true, label: i18n.account_app_on() },
		{ on: false, label: i18n.account_app_off() },
	];

	function pick(on: boolean) {
		if (on === current) return;
		save.mutate(on);
	}
</script>

<section class="rounded-lg border border-border bg-bg-elevated p-6">
	<header class="mb-4">
		<h3 class="text-base font-semibold text-fg">{i18n.account_title_logos()}</h3>
	</header>

	<fieldset
		class="min-w-0 inline-flex items-center rounded-md border border-border bg-bg p-0.5"
		aria-label={i18n.account_title_logos()}
	>
		{#each options as o (o.on)}
			<button
				type="button"
				onclick={() => pick(o.on)}
				aria-pressed={current === o.on}
				class={twMerge(
					"h-[34px] cursor-pointer rounded-sm px-4 text-sm font-medium transition focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-1",
					current === o.on
						? "bg-surface-2 text-fg"
						: "text-fg-subtle hover:text-fg",
				)}
			>
				{o.label}
			</button>
		{/each}
	</fieldset>

	<p class="mt-2.5 text-xs text-fg-subtle">{i18n.account_title_logos_help()}</p>
</section>
