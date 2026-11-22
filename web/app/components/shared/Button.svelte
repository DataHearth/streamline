<script lang="ts" module>
	const variants = {
		primary:
			"bg-accent text-fg-on-accent hover:bg-accent-hover hover:shadow-glow active:bg-accent-pressed disabled:shadow-none",
		secondary: "border-border-strong bg-surface text-fg hover:bg-surface-2",
		danger: "bg-status-failed text-bg-deep hover:bg-status-failed/90",
	};

	const sizes = {
		sm: "h-8 px-3 max-lg:min-h-11",
		md: "h-10 px-4 max-lg:min-h-11",
	};
</script>

<script lang="ts">
	import type { Snippet } from "svelte";
	import type { HTMLAnchorAttributes, HTMLButtonAttributes } from "svelte/elements";
	import { twMerge, type ClassNameValue } from "tailwind-merge";

	type Props = Omit<HTMLButtonAttributes & HTMLAnchorAttributes, "class"> & {
		variant?: keyof typeof variants;
		size?: keyof typeof sizes;
		class?: ClassNameValue;
		children: Snippet;
	};

	let { variant = "primary", size = "md", class: className, children, ...rest }: Props = $props();
</script>

<svelte:element
	this={rest.href ? "a" : "button"}
	{...rest}
	class={twMerge(
		"inline-flex items-center justify-center gap-1.5 whitespace-nowrap rounded-md border border-transparent text-sm font-semibold transition duration-150 active:scale-[0.97] disabled:cursor-not-allowed disabled:opacity-50",
		variants[variant],
		sizes[size],
		className,
	)}
>
	{@render children()}
</svelte:element>
