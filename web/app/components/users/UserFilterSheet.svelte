<script lang="ts">
	import { fly, fade } from "svelte/transition";
	import { cubicOut } from "svelte/easing";
	import { X, ArrowUp, ArrowDown } from "@lucide/svelte";
	import { cn } from "../../lib/cn";
	import { lockScroll, unlockScroll } from "../../lib/scrollLock";
	import { sheetSwipe } from "../../lib/sheet-swipe";
	import type { UserRole } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	// Sorting used to BE the table header, so it died with the table. This is
	// where it lives on touch: role filter and sort key in one sheet behind a
	// badged button, leaving the search field its own full-width line.
	export type UserSortKey = "name" | "role" | "auth" | "created";
	export type UserSortDir = "asc" | "desc";

	let {
		open,
		onClose,
		role,
		onRoleChange,
		sort,
		order,
		onSortChange,
		resultCount,
		activeCount = 0,
		onReset,
	}: {
		open: boolean;
		onClose: () => void;
		role: UserRole | "";
		onRoleChange: (r: UserRole | "") => void;
		sort: UserSortKey;
		order: UserSortDir;
		onSortChange: (k: UserSortKey) => void;
		resultCount: number;
		activeCount?: number;
		onReset: () => void;
	} = $props();

	$effect(() => {
		if (!open) return;
		lockScroll();
		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") onClose();
		};
		document.addEventListener("keydown", onKey);
		return () => {
			document.removeEventListener("keydown", onKey);
			unlockScroll();
		};
	});

	const chip =
		"inline-flex min-h-11 lg:h-9 lg:min-h-0 shrink-0 items-center gap-2 rounded-full border px-3.5 text-[13px] font-medium transition";
	const chipOff = "border-border bg-surface text-fg-muted active:bg-surface-2";
	const chipOn = "border-accent-line bg-accent-soft text-accent-text";
	const label =
		"mb-2.5 font-mono text-[9.5px] uppercase tracking-[0.16em] text-fg-faint";

	let roles = $derived<{ key: UserRole | ""; label: string }[]>([
		{ key: "", label: i18n.role_all() },
		{ key: "admin", label: i18n.common_admin() },
		{ key: "member", label: i18n.role_member() },
		{ key: "request_only", label: i18n.role_request_only() },
	]);

	let sorts = $derived<{ key: UserSortKey; label: string }[]>([
		{ key: "name", label: i18n.users_sort_name() },
		{ key: "role", label: i18n.common_role() },
		{ key: "auth", label: i18n.users_sort_auth() },
		{ key: "created", label: i18n.users_sort_created() },
	]);
</script>

{#if open}
	<div
		class="fixed inset-0 z-50 lg:hidden"
		role="dialog"
		aria-modal="true"
		aria-label={i18n.users_filter_sort()}
	>
		<button
			type="button"
			aria-label={i18n.common_close()}
			transition:fade={{ duration: 160 }}
			onclick={onClose}
			class="absolute inset-0 h-full w-full cursor-default bg-black/55"
		></button>

		<div
			use:sheetSwipe={{ onDismiss: onClose }}
			transition:fly={{ y: 420, duration: 280, easing: cubicOut }}
			class="absolute inset-x-0 bottom-0 flex max-h-[88dvh] flex-col overflow-hidden rounded-t-2xl border-t border-border-strong bg-bg-elevated shadow-4"
		>
			<div
				class="relative flex cursor-grab touch-none select-none items-center justify-between px-5 pb-2 pt-4 active:cursor-grabbing"
			>
				<span
					aria-hidden="true"
					class="absolute left-1/2 top-2 h-1 w-9 -translate-x-1/2 rounded-full bg-border-strong"
				></span>
				<h2 class="text-[17px] font-semibold tracking-tight text-fg">
					{i18n.users_filter_sort()}
				</h2>
				<button
					type="button"
					onclick={onClose}
					aria-label={i18n.common_close()}
					class="grid h-11 w-11 place-items-center rounded-full bg-surface text-fg-subtle transition active:bg-bg-hover"
				>
					<X size={16} aria-hidden="true" />
				</button>
			</div>

			<div
				data-sheet-scroll
				class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-5 pb-2 pt-3"
			>
				<div class={label}>{i18n.common_role()}</div>
				<div class="flex flex-wrap gap-2">
					{#each roles as r (r.key)}
						{@const on = role === r.key}
						<button
							type="button"
							aria-pressed={on}
							onclick={() => onRoleChange(r.key)}
							class={cn(chip, on ? chipOn : chipOff)}
						>
							{r.label}
						</button>
					{/each}
				</div>

				<div class="pt-5">
					<div class={label}>{i18n.common_sort_by()}</div>
					<div class="flex flex-wrap gap-2">
						{#each sorts as s (s.key)}
							{@const on = sort === s.key}
							<button
								type="button"
								aria-pressed={on}
								onclick={() => onSortChange(s.key)}
								class={cn(chip, on ? chipOn : chipOff)}
							>
								{s.label}
								{#if on}
									{#if order === "asc"}
										<ArrowUp size={13} aria-hidden="true" />
									{:else}
										<ArrowDown size={13} aria-hidden="true" />
									{/if}
								{/if}
							</button>
						{/each}
					</div>
					<p class="mt-2.5 text-[11.5px] text-fg-subtle">
						{i18n.users_sort_hint()}
					</p>
				</div>
			</div>

			<div
				class="flex items-center gap-2.5 border-t border-border px-5 pb-[max(env(safe-area-inset-bottom),14px)] pt-3.5"
			>
				<button
					type="button"
					disabled={activeCount === 0}
					onclick={onReset}
					class="inline-flex h-11 items-center justify-center rounded-xl px-4 text-[14px] font-medium text-fg-muted transition active:bg-surface disabled:opacity-40"
				>
					{i18n.common_reset()}
				</button>
				<button
					type="button"
					onclick={onClose}
					class="inline-flex h-11 flex-1 items-center justify-center rounded-xl bg-accent text-[14px] font-semibold text-fg-on-accent transition active:bg-accent-pressed"
				>
					{i18n.common_show_n({ n: resultCount })}
				</button>
			</div>
		</div>
	</div>
{/if}
