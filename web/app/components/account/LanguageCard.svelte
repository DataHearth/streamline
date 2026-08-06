<script lang="ts">
	import { cn } from "../../lib/cn";
	import { m as i18n } from "../../lib/paraglide/messages.js";
	import { getLocale, locales, setLocale } from "../../lib/paraglide/runtime.js";

	// Endonyms — a locale is always named in its own language, so "Français"
	// reads the same whichever locale the UI is currently in. Derived rather
	// than mapped by hand so a new entry in project.inlang/settings.json shows
	// up here without a code change.
	type Locale = (typeof locales)[number];

	function endonym(code: Locale): string {
		const name =
			new Intl.DisplayNames([code], { type: "language" }).of(code) ?? code;
		return name.charAt(0).toLocaleUpperCase(code) + name.slice(1);
	}

	const options = locales.map((code) => ({ code, label: endonym(code) }));

	let current = $state<Locale>(getLocale());

	// setLocale persists to localStorage and reloads. The reload is the
	// confirmation, so there is no separate success state to show.
	function pick(code: Locale) {
		if (code === current) return;
		current = code;
		setLocale(code);
	}
</script>

<section class="rounded-lg border border-border bg-bg-elevated p-6">
	<header class="mb-4">
		<h3 class="text-base font-semibold text-fg">{i18n.account_language()}</h3>
	</header>

	<div
		class="inline-flex items-center rounded-md border border-border bg-bg p-0.5"
		role="group"
		aria-label={i18n.account_language()}
	>
		{#each options as o (o.code)}
			<button
				type="button"
				onclick={() => pick(o.code)}
				aria-pressed={current === o.code}
				lang={o.code}
				class={cn(
					"h-[34px] cursor-pointer rounded-sm px-4 text-sm font-medium transition focus-visible:outline-2 focus-visible:outline-accent focus-visible:outline-offset-1",
					current === o.code
						? "bg-surface-2 text-fg"
						: "text-fg-subtle hover:text-fg",
				)}
			>
				{o.label}
			</button>
		{/each}
	</div>

	<p class="mt-2.5 text-xs text-fg-subtle">{i18n.account_language_help()}</p>
</section>
