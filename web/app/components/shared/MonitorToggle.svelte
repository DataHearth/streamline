<script lang="ts">
	import { Bookmark } from "@lucide/svelte";
	import { cn } from "@lib/cn";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The one bookmark: whether a movie, a show, a season, an episode, an
	// artist, an album, a book or a book series is monitored. Every placement
	// used to hand-roll its own button, and they drifted — some disabled while
	// saving and some did not, the hero ones disagreed on the "on" look, and a
	// few had a title but no accessible name. The look is chosen by placement
	// (variant + size), never by media type.
	type Props = {
		monitored: boolean;
		onToggle: (e: MouseEvent) => void;
		// The write is in flight: the button holds still rather than queue a
		// second toggle against a state the server has not answered yet.
		pending?: boolean;
		disabled?: boolean;
		// box: a bordered square that tints when on (heroes, panels, tiles).
		// ghost: a bare icon in a row (list rows, episodes).
		// overlay: the round chip on a poster's corner.
		variant?: "box" | "ghost" | "overlay";
		// Touch targets stay 44px below lg; the size picks the desktop one.
		// lg: 44 everywhere. md: 40 (the desktop heroes' button row).
		// sm: 36 on desktop. row: 32. xs: 28.
		size?: "lg" | "md" | "sm" | "row" | "xs";
		// A box laid over hero art: the off state is translucent so the art
		// shows through, as the buttons beside it do.
		backdrop?: boolean;
		// Accessible name when off / on, for a placement that names what it
		// monitors ("Monitor season 2").
		startLabel?: string;
		stopLabel?: string;
		class?: string;
	};

	let {
		monitored,
		onToggle,
		pending = false,
		disabled = false,
		variant = "box",
		size = "lg",
		backdrop = false,
		startLabel,
		stopLabel,
		class: klass = "",
	}: Props = $props();

	const SIZE = {
		lg: "h-11 w-11",
		md: "h-10 w-10",
		sm: "h-11 w-11 lg:h-9 lg:w-9",
		row: "h-11 w-11 lg:h-8 lg:w-8",
		xs: "h-11 w-11 lg:h-7 lg:w-7",
	} as const;
	const ICON = { lg: 16, md: 16, sm: 15, row: 15, xs: 14 } as const;

	let label = $derived(
		monitored ? (stopLabel ?? i18n.action_stop_monitoring()) : (startLabel ?? i18n.action_monitor()),
	);
</script>

<button
	type="button"
	onclick={onToggle}
	disabled={disabled || pending}
	aria-pressed={monitored}
	aria-busy={pending || undefined}
	aria-label={label}
	title={label}
	class={cn(
		"grid shrink-0 place-items-center transition focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring disabled:cursor-not-allowed disabled:opacity-50",
		variant === "overlay" ? "h-10 w-10 lg:h-7 lg:w-7" : SIZE[size],
		variant === "box" && [
			size === "md" || size === "xs" ? "rounded-md" : "rounded-lg",
			"border",
			monitored
				? "border-accent-line bg-accent-soft text-accent-text"
				: backdrop
					? "border-border-strong bg-white/[0.08] text-fg backdrop-blur-sm hover:bg-white/[0.14]"
					: "border-border bg-bg-elevated text-fg-subtle hover:border-border-strong hover:text-fg",
		],
		variant === "ghost" && [
			"rounded-md hover:bg-surface",
			monitored ? "text-accent-text" : "text-fg-subtle hover:text-fg",
		],
		variant === "overlay" && [
			"rounded-full border border-white/10 bg-bg-deep hover:bg-bg-elevated",
			monitored ? "text-accent-text" : "text-fg-subtle hover:text-fg",
		],
		klass,
	)}
>
	<Bookmark size={variant === "overlay" ? 14 : ICON[size]} fill={monitored ? "currentColor" : "none"} aria-hidden="true" />
</button>
