<script lang="ts">
	import { createQuery } from "@tanstack/svelte-query";
	import { Search } from "@lucide/svelte";
	import Modal from "@components/modals/Modal.svelte";
	import Select from "@components/forms/Select.svelte";
	import { api, errorText } from "@lib/api";
	import { fold } from "@lib/text";
	import { INPUT_CLASS } from "@lib/form";
	import type { CardigannCatalog, CardigannSummary } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The tracker definitions streamline runs itself: pick one and the add
	// form is generated from its settings.
	type Props = {
		open: boolean;
		onPick: (file: string) => void;
		onClose: () => void;
	};
	let { open, onPick, onClose }: Props = $props();

	const catalog = createQuery<CardigannCatalog>(() => ({
		queryKey: ["cardigann", "catalog"],
		queryFn: () => api<CardigannCatalog>("/cardigann/catalog"),
		enabled: open,
		staleTime: 5 * 60_000,
	}));

	let query = $state("");
	let privacy = $state("all");
	let language = $state("all");
	let category = $state("all");
	let login = $state("all");

	let definitions = $derived(catalog.data?.definitions ?? []);
	let languages = $derived(
		[...new Set(definitions.map((d) => d.language))].sort(),
	);
	let categories = $derived(
		[...new Set(definitions.flatMap((d) => d.categories))].sort(),
	);

	function loginMatches(d: CardigannSummary): boolean {
		switch (login) {
			case "none":
				return d.login === "none";
			case "credentials":
				return d.login !== "none" && !d.captcha;
			case "no-captcha":
				return !d.captcha;
			default:
				return true;
		}
	}

	let shown = $derived.by(() => {
		const q = fold(query.trim());
		return definitions.filter(
			(d) =>
				(privacy === "all" || d.type === privacy) &&
				(language === "all" || d.language === language) &&
				(category === "all" || d.categories.includes(category)) &&
				loginMatches(d) &&
				(q === "" ||
					fold(d.name).includes(q) ||
					fold(d.file).includes(q) ||
					fold(d.description).includes(q)),
		);
	});

	const all = { value: "all", label: i18n.cardigann_filter_all() };
</script>

<Modal {open} title={i18n.cardigann_catalog_title()} size="4xl" {onClose}>
	<div class="space-y-4">
		<p class="text-sm text-fg-muted">{i18n.cardigann_catalog_help()}</p>
		<div class="relative">
			<Search
				size={16}
				class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-fg-faint"
				aria-hidden="true"
			/>
			<input
				type="search"
				bind:value={query}
				placeholder={i18n.cardigann_catalog_search()}
				aria-label={i18n.cardigann_catalog_search()}
				class="{INPUT_CLASS} pl-9"
			/>
		</div>
		<div class="grid gap-3 sm:grid-cols-4">
			<Select
				label={i18n.cardigann_filter_privacy()}
				value={privacy}
				onChange={(v) => (privacy = v)}
				readOnlyExempt
				options={[
					all,
					{ value: "public", label: i18n.cardigann_type_public() },
					{ value: "semi-private", label: i18n.cardigann_type_semi() },
					{ value: "private", label: i18n.cardigann_type_private() },
				]}
			/>
			<Select
				label={i18n.cardigann_filter_language()}
				value={language}
				onChange={(v) => (language = v)}
				readOnlyExempt
				options={[all, ...languages.map((l) => ({ value: l, label: l }))]}
			/>
			<Select
				label={i18n.cardigann_filter_category()}
				value={category}
				onChange={(v) => (category = v)}
				readOnlyExempt
				options={[all, ...categories.map((c) => ({ value: c, label: c }))]}
			/>
			<Select
				label={i18n.cardigann_filter_login()}
				value={login}
				onChange={(v) => (login = v)}
				readOnlyExempt
				options={[
					all,
					{ value: "none", label: i18n.cardigann_login_none() },
					{ value: "credentials", label: i18n.cardigann_login_credentials() },
					{ value: "no-captcha", label: i18n.cardigann_login_no_captcha() },
				]}
			/>
		</div>

		{#if catalog.isPending}
			<p class="text-sm text-fg-muted">{i18n.common_loading()}</p>
		{:else if catalog.isError}
			<p class="text-sm text-status-failed">{errorText(catalog.error)}</p>
		{:else}
			<p class="text-xs text-fg-subtle">
				{i18n.cardigann_catalog_count({ shown: shown.length, total: definitions.length })}
			</p>
			<ul class="max-h-[55vh] divide-y divide-border overflow-y-auto rounded-md border border-border">
				{#each shown as d (d.file)}
					<li>
						<button
							type="button"
							onclick={() => onPick(d.file)}
							class="flex w-full flex-col gap-1 px-4 py-3 text-left transition hover:bg-surface"
						>
							<span class="flex flex-wrap items-center gap-2">
								<span class="text-sm font-semibold text-fg">{d.name}</span>
								<span class="text-[11px] text-fg-subtle">{d.language}</span>
								<span
									class="rounded-full bg-surface px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-fg-muted"
									>{d.type}</span
								>
								{#if d.login !== "none"}
									<span
										class="rounded-full bg-status-seeding/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-status-seeding"
										>{i18n.cardigann_badge_login()}</span
									>
								{/if}
								{#if d.captcha}
									<span
										class="rounded-full bg-status-wanted/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-status-wanted"
										>{i18n.cardigann_badge_captcha()}</span
									>
								{/if}
								{#if d.challenge}
									<span
										class="rounded-full bg-status-wanted/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-status-wanted"
										>{i18n.cardigann_badge_challenge()}</span
									>
								{/if}
								{#if d.custom}
									<span
										class="rounded-full bg-accent-soft px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-accent-text"
										>{i18n.cardigann_badge_custom()}</span
									>
								{/if}
							</span>
							<span class="line-clamp-2 text-xs text-fg-muted">{d.description}</span>
							{#if d.categories.length > 0}
								<span class="text-[11px] text-fg-faint">{d.categories.join(" · ")}</span>
							{/if}
						</button>
					</li>
				{:else}
					<li class="px-4 py-6 text-center text-sm text-fg-muted">
						{i18n.cardigann_catalog_empty()}
					</li>
				{/each}
			</ul>
		{/if}
	</div>
</Modal>
