<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { RefreshCw } from "@lucide/svelte";
	import Dialog from "@components/modals/Dialog.svelte";
	import { api, errorText } from "@lib/api";
	import { toast } from "@lib/toast";
	import type { Indexer, IndexerCaptcha } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The captcha round trip of a built-in indexer whose tracker shows one:
	// the image is fetched with the login page it belongs to, and the answer
	// goes back with the token that page is held under.
	type Props = { indexer: Indexer | null; onClose: () => void };
	let { indexer, onClose }: Props = $props();

	const qc = useQueryClient();
	let captcha = $state<IndexerCaptcha | null>(null);
	let answer = $state("");
	let loading = $state(false);
	let loadError = $state<string | null>(null);

	async function load(name: string) {
		loading = true;
		loadError = null;
		captcha = null;
		answer = "";
		try {
			captcha = await api<IndexerCaptcha | null>(
				`/indexers/${encodeURIComponent(name)}/captcha`,
				{ method: "POST" },
			);
		} catch (err) {
			loadError = errorText(err);
		} finally {
			loading = false;
		}
	}

	$effect(() => {
		if (indexer) load(indexer.name);
	});

	const login = createMutation<Indexer, Error, string>(() => ({
		mutationFn: (name) =>
			api<Indexer>(`/indexers/${encodeURIComponent(name)}/login`, {
				method: "POST",
				body: captcha ? { token: captcha.token, answer } : {},
			}),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["indexers"] });
			toast.ok(i18n.indexer_logged_in());
			onClose();
		},
		onError: (err) => {
			toast.err(errorText(err));
			// The page the answer was for is spent either way.
			if (indexer) load(indexer.name);
		},
	}));
</script>

<Dialog
	open={indexer !== null}
	title={i18n.indexer_login_title({ name: indexer?.name ?? "" })}
	{onClose}
	actions={[
		{ label: i18n.common_cancel(), variant: "ghost" },
		{
			label: i18n.indexer_login(),
			variant: "primary",
			dismiss: false,
			pending: login.isPending,
			disabled: loading || loadError !== null || (captcha !== null && !answer.trim()),
			onClick: () => indexer && login.mutate(indexer.name),
		},
	]}
>
	{#if loading}
		<p class="text-sm text-fg-muted">{i18n.indexer_captcha_loading()}</p>
	{:else if loadError}
		<p class="text-sm text-status-failed">{loadError}</p>
	{:else if captcha}
		<div class="space-y-3">
			<p class="text-sm text-fg-muted">{i18n.indexer_captcha_help()}</p>
			<div class="flex items-center gap-2">
				<img
					src={captcha.image}
					alt={i18n.indexer_captcha_alt()}
					class="max-h-24 rounded border border-border bg-white"
				/>
				<button
					type="button"
					onclick={() => indexer && load(indexer.name)}
					class="grid h-9 w-9 place-items-center rounded-md text-fg-muted transition hover:bg-surface hover:text-fg"
					aria-label={i18n.indexer_captcha_reload()}
				>
					<RefreshCw size={16} aria-hidden="true" />
				</button>
			</div>
			<input
				type="text"
				bind:value={answer}
				autocomplete="off"
				aria-label={i18n.indexer_captcha_answer()}
				class="w-full rounded-md border border-border bg-bg-elevated px-3 py-2 text-sm text-fg focus:border-accent focus:outline-none"
			/>
		</div>
	{:else}
		<p class="text-sm text-fg-muted">{i18n.indexer_captcha_none()}</p>
	{/if}
</Dialog>
