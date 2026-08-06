<script lang="ts">
	import { ExternalLink, Trash2 } from "@lucide/svelte";
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { auth } from "../../lib/auth.svelte";
	import { api, errorText } from "../../lib/api";
	import { toast } from "../../lib/toast";
	import { formatBytes } from "../../lib/format";
	import Dialog from "../modals/Dialog.svelte";
	import Checkbox from "../forms/Checkbox.svelte";
	import type { Movie } from "../../lib/types";
	import { m as i18n } from "../../lib/paraglide/messages.js";

	let {
		movie,
		qualityProfileName,
	}: {
		movie: Movie;
		qualityProfileName: string | null;
	} = $props();

	let primary = $derived(movie.media_files?.[0]);

	// The path is far wider than the column and truncates. Desktop has the
	// title tooltip; touch has neither hover nor room, so a press and hold
	// puts the whole path on the clipboard — the same 480ms/8px gesture the
	// poster grid uses for selection.
	const HOLD_MS = 480;
	const HOLD_SLOP = 8;
	let holdTimer: number | null = null;
	let holdX = 0;
	let holdY = 0;
	let holding = $state(false);
	let longPressed = false;
	let touchGesture = false;

	async function copyPath() {
		if (!primary?.path) return;
		try {
			await navigator.clipboard.writeText(primary.path);
			toast.ok("Path copied");
		} catch {
			toast.err("Clipboard unavailable");
		}
	}
	function cancelHold() {
		holding = false;
		if (holdTimer !== null) {
			clearTimeout(holdTimer);
			holdTimer = null;
		}
	}
	function onPointerDown(e: PointerEvent) {
		longPressed = false;
		touchGesture = e.pointerType !== "mouse";
		if (!touchGesture) return;
		cancelHold();
		holdX = e.clientX;
		holdY = e.clientY;
		holding = true;
		holdTimer = window.setTimeout(() => {
			holdTimer = null;
			holding = false;
			longPressed = true;
			navigator.vibrate?.(10);
			copyPath();
		}, HOLD_MS);
	}
	function onPointerMove(e: PointerEvent) {
		if (holdTimer === null) return;
		if (
			Math.abs(e.clientX - holdX) > HOLD_SLOP ||
			Math.abs(e.clientY - holdY) > HOLD_SLOP
		)
			cancelHold();
	}
	// A tap is not a copy — touch waits for the hold. Keyboard activation
	// (detail 0) always copies.
	function onClick(e: MouseEvent) {
		if (e.detail === 0 || !touchGesture) copyPath();
	}
	function onContextMenu(e: MouseEvent) {
		// Android raises its own menu on the same gesture.
		if (longPressed || holding) e.preventDefault();
	}

	const qc = useQueryClient();
	let deleteOpen = $state(false);
	let removeTorrent = $state(false);

	const del = createMutation<unknown, Error, { fileId: number; remove: boolean }>(
		() => ({
			mutationFn: ({ fileId, remove }) =>
				api(`/movies/${movie.id}/files/${fileId}`, {
					method: "DELETE",
					body: { remove_torrent: remove },
				}),
			onSuccess: () => {
				qc.invalidateQueries({ queryKey: ["movie", movie.id] });
				toast.ok("File deleted");
				deleteOpen = false;
			},
			onError: (e) => toast.err(errorText(e, i18n.common_delete_failed())),
		}),
	);
</script>

<aside class="flex flex-col gap-4">
	<section
		class="rounded-lg border border-border bg-bg-elevated p-5"
		aria-labelledby="info-file"
	>
		<div class="flex items-center justify-between">
			<h4
				id="info-file"
				class="font-mono text-[11px] uppercase tracking-[0.14em] text-fg-faint"
			>
				{i18n.common_file()}
			</h4>
			{#if primary && auth.canAddDirectly}
				<button
					type="button"
					onclick={() => {
						removeTorrent = false;
						deleteOpen = true;
					}}
					aria-label={i18n.action_delete_file()}
					title={i18n.action_delete_file()}
					class="grid h-7 w-7 place-items-center rounded-md text-fg-muted transition hover:bg-status-failed/10 hover:text-status-failed focus:outline-none focus:ring-2 focus:ring-accent-ring"
				>
					<Trash2 class="h-4 w-4" aria-hidden="true" />
				</button>
			{/if}
		</div>
		{#if primary}
			<dl class="mt-3 grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-[12px]">
				<dt class="text-fg-subtle">{i18n.file_resolution()}</dt>
				<dd class="text-right font-mono text-fg">
					{primary.parsed_resolution || "—"}
				</dd>
				<dt class="text-fg-subtle">{i18n.file_codec()}</dt>
				<dd class="text-right font-mono text-fg">
					{primary.parsed_codec || "—"}
				</dd>
				<dt class="text-fg-subtle">{i18n.file_source()}</dt>
				<dd class="text-right font-mono text-fg">
					{primary.parsed_source || "—"}
				</dd>
				<dt class="text-fg-subtle">{i18n.common_size()}</dt>
				<dd class="text-right font-mono text-fg">
					{formatBytes(primary.size)}
				</dd>
				<dt class="text-fg-subtle">{i18n.file_group()}</dt>
				<dd class="text-right font-mono text-fg">
					{primary.release_group || "—"}
				</dd>
				<dt class="text-fg-subtle">{i18n.field_path()}</dt>
				<dd class="min-w-0">
					<button
						type="button"
						onpointerdown={onPointerDown}
						onpointermove={onPointerMove}
						onpointerup={cancelHold}
						onpointercancel={cancelHold}
						oncontextmenu={onContextMenu}
						onclick={onClick}
						title={primary.path}
						aria-label={i18n.action_copy_file_path()}
						class="block w-full truncate rounded text-right font-mono underline decoration-border-strong decoration-dotted underline-offset-[3px] transition-colors [-webkit-touch-callout:none] focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring {holding
							? 'text-accent-text'
							: 'text-fg'}"
					>
						{primary.path}
					</button>
				</dd>
			</dl>
		{:else}
			<p class="mt-3 text-[12px] text-fg-subtle">
				No file yet.{movie.status === "wanted"
					? " Searching nightly."
					: ""}
			</p>
		{/if}
	</section>

	<section
		class="rounded-lg border border-border bg-bg-elevated p-5"
		aria-labelledby="info-library"
	>
		<h4
			id="info-library"
			class="font-mono text-[11px] uppercase tracking-[0.14em] text-fg-faint"
		>
			{i18n.nav_library()}
		</h4>
		<dl class="mt-3 grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-[12px]">
			<dt class="text-fg-subtle">{i18n.quality_profile()}</dt>
			<dd class="text-right font-mono text-fg">
				{qualityProfileName ?? "—"}
			</dd>
			<dt class="text-fg-subtle">{i18n.common_status()}</dt>
			<dd class="text-right font-mono text-fg capitalize">
				{movie.status}
			</dd>
			<dt class="text-fg-subtle">{i18n.monitor_monitored()}</dt>
			<dd class="text-right font-mono text-fg">
				{movie.monitored ? i18n.common_yes() : i18n.common_no()}
			</dd>
			{#if movie.year}
				<dt class="text-fg-subtle">{i18n.common_year()}</dt>
				<dd class="text-right font-mono text-fg">{movie.year}</dd>
			{/if}
			{#if movie.runtime}
				<dt class="text-fg-subtle">{i18n.detail_runtime()}</dt>
				<dd class="text-right font-mono text-fg">{movie.runtime}m</dd>
			{/if}
			<dt class="text-fg-subtle">TMDB</dt>
			<dd class="text-right">
				<a
					href="https://www.themoviedb.org/movie/{movie.tmdb_id}"
					target="_blank"
					rel="noopener noreferrer"
					class="inline-flex items-center gap-1 font-mono text-accent-text transition hover:text-accent"
				>
					{movie.tmdb_id}
					<ExternalLink size={11} aria-hidden="true" />
				</a>
			</dd>
		</dl>
	</section>
</aside>

<Dialog
	open={deleteOpen}
	title={i18n.file_delete_confirm()}
	onClose={() => (deleteOpen = false)}
	actions={[
		{ label: i18n.common_cancel(), variant: "ghost", autofocus: true },
		{
			label: i18n.action_delete_file(),
			variant: "danger",
			dismiss: false,
			pending: del.isPending,
			onClick: () =>
				primary && del.mutate({ fileId: primary.id, remove: removeTorrent }),
		},
	]}
>
	<p class="text-sm leading-relaxed text-fg-muted">
		{i18n.file_removed_reverts()} <span
			class="font-medium text-fg">wanted</span
		>, so the next monitored search re-grabs it.
	</p>
	<Checkbox
		checked={removeTorrent}
		onChange={(v) => (removeTorrent = v)}
		class="mt-4 text-sm text-fg"
	>
		{i18n.file_also_remove_torrent()}
	</Checkbox>
</Dialog>
