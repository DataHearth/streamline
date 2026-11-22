<script lang="ts">
	import BottomSheet from "@components/shared/BottomSheet.svelte";
	import { ArrowUp, ArrowDown } from "@lucide/svelte";
	import { twMerge } from "tailwind-merge";
	import type { UserRole } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Below lg there is no table, so no header to sort by. This is where
	// sorting lives on touch: role filter and sort key in one sheet behind a
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


	const chip =
		"inline-flex min-h-11 lg:h-9 lg:min-h-0 shrink-0 items-center gap-2 rounded-full border px-3.5 text-[13px] font-medium transition";
	const chipOff = "border-border bg-surface text-fg-muted active:bg-surface-2";
	const chipOn = "border-accent-line bg-accent-soft text-accent-text";
	const label =
		"mb-2.5 eyebrow text-[9.5px] text-fg-subtle";

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
	<BottomSheet
		title={i18n.users_filter_sort()}
		label={i18n.users_filter_sort()}
		{onClose}
		bodyClass="pt-3"
	>
		<div class={label}>{i18n.common_role()}</div>
		<div class="flex flex-wrap gap-2">
			{#each roles as r (r.key)}
				{@const on = role === r.key}
				<button
					type="button"
					aria-pressed={on}
					onclick={() => onRoleChange(r.key)}
					class={twMerge(chip, on ? chipOn : chipOff)}
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
						class={twMerge(chip, on ? chipOn : chipOff)}
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
		{#snippet footer()}
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
		{/snippet}
	</BottomSheet>
{/if}
