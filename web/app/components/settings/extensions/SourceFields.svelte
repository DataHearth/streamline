<script lang="ts">
	import { cn } from "@lib/cn";
	import { isUrlSource, shaProblem, sourceProblem } from "@lib/extensions";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The two things an admin copies from an extension's release notes. Both
	// stay editable on a read-only instance: reading a module saves nothing,
	// and the edit ends on the YAML to paste.
	let {
		source = $bindable(""),
		sha256 = $bindable(""),
		// Set once a submit was tried, so every problem shows, not only the
		// fields already left.
		showErrors = false,
		idPrefix,
	}: {
		source?: string;
		sha256?: string;
		showErrors?: boolean;
		idPrefix: string;
	} = $props();

	let touched = $state({ source: false, sha256: false });
	let srcError = $derived(showErrors || touched.source ? sourceProblem(source) : null);
	let shaError = $derived(showErrors || touched.sha256 ? shaProblem(sha256, source) : null);
	// The checksum is required for a URL and optional for a path on the server:
	// the label says which, as soon as the source says.
	let shaOptional = $derived(!!source.trim() && !isUrlSource(source));

	const input =
		"w-full rounded-md border bg-bg px-3 py-2 font-mono text-[13px] text-fg placeholder:font-sans placeholder:text-fg-faint focus:outline-none focus-visible:ring-2 focus-visible:ring-accent";
</script>

<div class="flex flex-col gap-4">
	<div>
		<label for="{idPrefix}-source" class="mb-1 block text-sm font-medium text-fg">{i18n.extensions_source()}</label>
		<input
			id="{idPrefix}-source"
			type="text"
			inputmode="url"
			autocomplete="off"
			autocapitalize="off"
			spellcheck="false"
			placeholder={i18n.extensions_source_placeholder()}
			bind:value={source}
			onblur={() => (touched.source = true)}
			aria-invalid={srcError ? true : undefined}
			aria-describedby="{idPrefix}-source-help"
			class={cn(input, srcError ? "border-status-failed" : "border-border")}
		/>
		<p id="{idPrefix}-source-help" class={cn("mt-1 text-xs", srcError ? "text-status-failed" : "text-fg-muted")}>
			{srcError ?? i18n.extensions_source_help()}
		</p>
	</div>
	<div>
		<label for="{idPrefix}-sha" class="mb-1 flex items-baseline gap-1.5 text-sm font-medium text-fg">
			SHA-256
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
			bind:value={sha256}
			onblur={() => (touched.sha256 = true)}
			aria-invalid={shaError ? true : undefined}
			aria-describedby="{idPrefix}-sha-help"
			class={cn(input, shaError ? "border-status-failed" : "border-border")}
		/>
		<p id="{idPrefix}-sha-help" class={cn("mt-1 text-xs", shaError ? "text-status-failed" : "text-fg-muted")}>
			{shaError ?? i18n.extensions_sha256_help()}
		</p>
	</div>
</div>
