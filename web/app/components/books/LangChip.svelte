<script lang="ts">
	import { cn } from "@lib/cn";
	import { languageName } from "@lib/music-books";

	// The two-letter language code, outlined; accent when it is the edition in use.
	let { code, active = false }: { code: string; active?: boolean } = $props();
</script>

<span
	class={cn(
		"inline-flex h-5 shrink-0 items-center rounded border px-1.5 font-mono text-[10.5px] font-semibold uppercase",
		active ? "border-accent-line text-accent-text" : "border-border-strong text-fg-muted",
	)}
	title={languageName(code, true)}
>
	{code}
	<span class="sr-only">{languageName(code, true)}</span>
</span>
