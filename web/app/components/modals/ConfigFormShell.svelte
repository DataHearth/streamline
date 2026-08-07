<script lang="ts">
	import type { Snippet } from "svelte";
	import { fade } from "svelte/transition";
	import { config } from "../../lib/config.svelte";
	import { lockScroll, unlockScroll } from "../../lib/scrollLock";
	import { initialFocusTarget, portal, trapFocus } from "../../lib/focus-trap";
	import { createSettingsDesktop } from "../../lib/viewport.svelte";
	import Modal from "./Modal.svelte";
	import ConfigModalFooter from "../settings/ConfigModalFooter.svelte";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// The five config forms — indexer, download client, media server, quality
	// profile, SSO — are nine to twelve fields plus a connection test. As a
	// centred max-w-2xl card with 8px gutters and a max-h-[85vh] cap they were a
	// scrolling box inside a scrolling page, 74px shorter than the screen and
	// showing nothing useful behind them.
	//
	// From lg they stay exactly that (a modal over the card list you were just
	// reading). Below lg the form takes the screen: Cancel / title / Save as a
	// bar, the fields at full width, and the connection test rendered inline
	// under the fields it is testing rather than as a toast over a scrim.
	type Props = {
		open: boolean;
		title: string;
		formId: string;
		submitLabel: string;
		submitDisabled?: boolean;
		size?: "md" | "lg" | "xl" | "2xl" | "3xl" | "4xl";
		onClose: () => void;
		children: Snippet;
		// Receives the layout it should render in: the desktop footer wants the
		// compact inline button, the touch body a full-width one that can print
		// the failure reason underneath.
		test?: Snippet<[("inline" | "block")]>;
		// A static line of context (e.g. "changes apply after restart") for forms
		// that have no connection to test.
		note?: Snippet;
	};

	let {
		open,
		title,
		formId,
		submitLabel,
		submitDisabled = false,
		size = "xl",
		onClose,
		children,
		test,
		note,
	}: Props = $props();

	const desktop = createSettingsDesktop();

	let panel = $state<HTMLDivElement | null>(null);
	let lastFocused: HTMLElement | null = null;
	let titleId = `config-form-${Math.random().toString(36).slice(2, 10)}`;

	let fullScreen = $derived(open && !desktop());

	$effect(() => {
		if (!fullScreen) {
			if (lastFocused) {
				lastFocused.focus();
				lastFocused = null;
			}
			return;
		}
		lastFocused = document.activeElement as HTMLElement | null;
		lockScroll();
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") {
				e.stopPropagation();
				onClose();
			}
		};
		document.addEventListener("keydown", onKey);
		requestAnimationFrame(() => {
			if (panel) initialFocusTarget(panel)?.focus();
		});
		return () => {
			document.removeEventListener("keydown", onKey);
			unlockScroll();
		};
	});
</script>

{#if fullScreen}
	<div
		use:portal
		class="fixed inset-0 z-50"
		transition:fade={{ duration: 120 }}
	>
		<div
			bind:this={panel}
			use:trapFocus
			role="dialog"
			aria-modal="true"
			aria-labelledby={titleId}
			class="flex h-full w-full flex-col bg-bg-deep"
		>
			<header
				class="flex h-14 shrink-0 items-center gap-2 border-b border-border bg-bg-elevated px-3"
			>
				<button
					type="button"
					onclick={onClose}
					class="shrink-0 rounded-md px-2 py-2 text-sm text-fg-muted transition active:bg-bg-hover"
				>
					{config.readOnly ? i18n.common_close() : i18n.common_cancel()}
				</button>
				<h2
					id={titleId}
					class="min-w-0 flex-1 truncate text-center text-[15px] font-semibold tracking-tight text-fg"
				>
					{title}
				</h2>
				{#if config.readOnly}
					<span class="w-14 shrink-0" aria-hidden="true"></span>
				{:else}
					<button
						type="submit"
						form={formId}
						disabled={submitDisabled}
						class="shrink-0 rounded-md px-2 py-2 text-sm font-semibold text-accent-text transition active:bg-bg-hover disabled:opacity-40"
					>
						{submitLabel}
					</button>
				{/if}
			</header>

			<div class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-4 py-4">
				{@render children()}
				{#if test || note}
					<div class="mt-5 border-t border-border pt-5">
						{@render test?.("block")}
						{@render note?.()}
					</div>
				{/if}
				<div class="h-[max(env(safe-area-inset-bottom),16px)]"></div>
			</div>
		</div>
	</div>
{:else}
	<Modal {open} {title} {size} {onClose}>
		{@render children()}
		{#snippet footer()}
			<ConfigModalFooter
				{formId}
				{submitLabel}
				{submitDisabled}
				onCancel={onClose}
			>
				{#snippet left()}
					{#if test}
						<div class="sm:mr-auto">{@render test("inline")}</div>
					{/if}
					{@render note?.()}
				{/snippet}
			</ConfigModalFooter>
		{/snippet}
	</Modal>
{/if}
