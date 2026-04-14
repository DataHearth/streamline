<script lang="ts">
	import { Sparkles, Download, KeyRound } from "@lucide/svelte";

	type Props = {
		name: string;
		size?: number;
		ariaLabel?: string;
	};

	let { name, size = 20, ariaLabel }: Props = $props();

	const KNOWN_SVG = new Set([
		"plex",
		"jellyfin",
		"emby",
		"qbittorrent",
		"transmission",
		"deluge",
		"authentik",
		"keycloak",
		"authelia",
		"google",
		"okta",
		"prowlarr",
		"jackett",
		"nzbhydra2",
	]);

	// TODO: vendor SVGs for these names into web/static/images/brand-logos/
	// and move the slug into KNOWN_SVG. Lucide fallback in the meantime.
	const FALLBACK_ICON: Record<string, typeof Sparkles> = {
		sabnzbd: Download,
		nzbget: Download,
		azure: KeyRound,
	};

	const slug = $derived(name.toLowerCase());
	const hasSvg = $derived(KNOWN_SVG.has(slug));
	const fallbackIcon = $derived(FALLBACK_ICON[slug]);
</script>

{#if hasSvg}
	<img
		src="/static/images/brand-logos/{slug}.svg"
		width={size}
		height={size}
		alt={ariaLabel ?? ""}
		aria-hidden={ariaLabel ? undefined : "true"}
		loading="lazy"
		decoding="async"
		class="inline-block object-contain"
	/>
{:else if fallbackIcon}
	{@const Icon = fallbackIcon}
	<Icon
		{size}
		class="inline-block text-fg-faint"
		aria-label={ariaLabel}
		aria-hidden={ariaLabel ? undefined : "true"}
	/>
{:else}
	<Sparkles
		{size}
		class="inline-block text-fg-faint"
		aria-label={ariaLabel}
		aria-hidden={ariaLabel ? undefined : "true"}
	/>
{/if}
