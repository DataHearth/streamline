<script lang="ts">
	import { onMount } from "svelte";
	import { twMerge } from "tailwind-merge";
	import FieldLock from "@components/forms/FieldLock.svelte";
	import { isUrlSource, shaProblem, sourceProblem } from "@lib/extensions";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The two things an admin copies from an extension's release notes. Adding
	// one on a read-only instance keeps them editable, since that flow ends on
	// the YAML to paste; editing an existing entry there locks them (`locked`).
	let {
		source = $bindable(""),
		sha256 = $bindable(""),
		// Set once a submit was tried, so every problem shows, not only the
		// fields already left.
		showErrors = false,
		idPrefix,
		locked = false,
	}: {
		source?: string;
		sha256?: string;
		showErrors?: boolean;
		idPrefix: string;
		locked?: boolean;
	} = $props();

	let touched = $state({ source: false, sha256: false });

	// The dialog focuses this field once it opens, and a value set from script
	// leaves the caret at its end, which scrolls a long URL to its file name.
	// The host is what tells one source from another.
	let sourceInput!: HTMLInputElement;
	onMount(() => sourceInput.setSelectionRange(0, 0));
	let srcError = $derived(showErrors || touched.source ? sourceProblem(source) : null);
	let shaError = $derived(showErrors || touched.sha256 ? shaProblem(sha256, source) : null);
	// The checksum is required for a URL and optional for a path on the server:
	// the label says which, as soon as the source says.
	let shaOptional = $derived(!!source.trim() && !isUrlSource(source));

	const input =
		"w-full rounded-md border bg-bg px-3 py-2 font-mono text-[13px] text-fg placeholder:font-sans placeholder:text-fg-faint focus:outline-none focus-visible:ring-2 focus-visible:ring-accent read-only:cursor-not-allowed read-only:opacity-70 read-only:caret-transparent read-only:focus-visible:ring-0";
</script>

<div class="flex flex-col gap-4">
	<div>
		<label for="{idPrefix}-source" class="mb-1 flex items-center gap-1.5 text-sm font-medium text-fg"
			>{i18n.extensions_source()}<FieldLock {locked} /></label
		>
		<input
			id="{idPrefix}-source"
			type="text"
			inputmode="url"
			autocomplete="off"
			autocapitalize="off"
			spellcheck="false"
			placeholder={i18n.extensions_source_placeholder()}
			readonly={locked}
			bind:this={sourceInput}
			bind:value={source}
			onblur={() => (touched.source = true)}
			aria-invalid={srcError ? true : undefined}
			aria-describedby="{idPrefix}-source-help"
			class={twMerge(input, srcError ? "border-status-failed" : "border-border")}
		/>
		<p id="{idPrefix}-source-help" class={twMerge("mt-1 text-xs", srcError ? "text-status-failed" : "text-fg-muted")}>
			{srcError ?? i18n.extensions_source_help()}
		</p>
	</div>
	<div>
		<label for="{idPrefix}-sha" class="mb-1 flex items-baseline gap-1.5 text-sm font-medium text-fg">
			SHA-256<FieldLock {locked} />
			{#if shaOptional}
				<span class="text-xs font-normal text-fg-subtle">{i18n.extensions_sha256_optional()}</span>
			{/if}
		</label>
		<input
			id="{idPrefix}-sha"
			type="text"
			autocomplete="off"
			autocapitalize="off"
			spellcheck="false"
			maxlength={64}
			placeholder={i18n.extensions_sha256_placeholder()}
			readonly={locked}
			bind:value={sha256}
			onblur={() => (touched.sha256 = true)}
			aria-invalid={shaError ? true : undefined}
			aria-describedby="{idPrefix}-sha-help"
			class={twMerge(input, shaError ? "border-status-failed" : "border-border")}
		/>
		<p id="{idPrefix}-sha-help" class={twMerge("mt-1 text-xs", shaError ? "text-status-failed" : "text-fg-muted")}>
			{shaError ?? i18n.extensions_sha256_help()}
		</p>
	</div>
</div>
