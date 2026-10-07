<script lang="ts">
	import {
		createMutation,
		createQuery,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { Bookmark, ChevronRight, Disc3 } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { auth } from "@lib/auth.svelte";
	import { cn } from "@lib/cn";
	import { albumPosterUrl } from "@lib/posters";
	import { albumStatus } from "@lib/status";
	import { toast } from "@lib/toast";
	import Poster from "@components/shared/Poster.svelte";
	import StatusPill from "@components/shared/StatusPill.svelte";
	import type { MusicAlbum } from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	let {
		album,
		open,
		onToggle,
	}: {
		album: MusicAlbum;
		open: boolean;
		onToggle: () => void;
	} = $props();

	const qc = useQueryClient();

	const detail = createQuery<MusicAlbum>(() => ({
		queryKey: ["music", "album", album.id],
		queryFn: () => api<MusicAlbum>(`/music/albums/${album.id}`),
		enabled: open,
	}));

	const monitor = createMutation<MusicAlbum, Error, boolean>(() => ({
		mutationFn: (monitored) =>
			api<MusicAlbum>(`/music/albums/${album.id}`, {
				method: "PATCH",
				body: { monitored },
			}),
		onSuccess: () => qc.invalidateQueries({ queryKey: ["music"] }),
		onError: (e) => toast.err(errorText(e, i18n.common_update_failed())),
	}));

	function clock(seconds: number): string {
		const s = String(seconds % 60).padStart(2, "0");
		return `${Math.floor(seconds / 60)}:${s}`;
	}

	let year = $derived(album.release_date?.slice(0, 4) ?? "");
	let tracks = $derived(detail.data?.tracks ?? []);
</script>

<li class="rounded-lg border border-border bg-bg-elevated">
	<div class="flex items-center gap-3 p-3">
		<button
			type="button"
			onclick={onToggle}
			aria-expanded={open}
			class="flex min-w-0 flex-1 items-center gap-3 text-left"
		>
			<ChevronRight
				size={16}
				class={cn("shrink-0 text-fg-faint transition", open && "rotate-90")}
				aria-hidden="true"
			/>
			<span
				class="relative grid h-12 w-12 shrink-0 place-items-center overflow-hidden rounded bg-bg-card text-fg-faint"
			>
				<Disc3 size={20} aria-hidden="true" />
				<Poster
					src={albumPosterUrl(album.id)}
					alt={i18n.music_cover_alt({ title: album.title })}
					class="absolute inset-0 h-full w-full object-cover"
				/>
			</span>
			<span class="min-w-0 flex-1">
				<span class="block truncate text-sm font-semibold text-fg">
					{album.title}
				</span>
				<span class="mt-0.5 block text-xs text-fg-muted">
					{#if year}{year} · {/if}{album.track_count === 1
						? i18n.music_tracks_count_one({ count: 1 })
						: i18n.music_tracks_count_other({ count: album.track_count })}
				</span>
			</span>
		</button>
		<StatusPill status={albumStatus(album)} size="sm" />
		{#if auth.canAddDirectly}
			<button
				type="button"
				onclick={() => monitor.mutate(!album.monitored)}
				disabled={monitor.isPending}
				aria-pressed={album.monitored}
				aria-label={album.monitored
					? i18n.action_stop_monitoring()
					: i18n.action_monitor()}
				title={album.monitored
					? i18n.action_stop_monitoring()
					: i18n.action_monitor()}
				class={cn(
					"grid h-9 w-9 shrink-0 place-items-center rounded-md border transition disabled:opacity-60",
					album.monitored
						? "border-accent-line bg-accent-soft text-accent-text"
						: "border-border-strong text-fg-muted hover:text-fg",
				)}
			>
				<Bookmark
					size={15}
					fill={album.monitored ? "currentColor" : "none"}
					aria-hidden="true"
				/>
			</button>
		{/if}
	</div>

	{#if open}
		<div class="border-t border-border px-3 py-2">
			{#if detail.isPending}
				<p class="py-2 text-xs text-fg-muted" role="status">
					{i18n.common_loading()}
				</p>
			{:else if detail.isError}
				<p class="py-2 text-xs text-status-failed">
					{i18n.err_load_failed_detail({ reason: errorText(detail.error) })}
				</p>
			{:else if tracks.length === 0}
				<p class="py-2 text-xs text-fg-muted">{i18n.music_no_tracks()}</p>
			{:else}
				<table class="w-full text-left text-xs">
					<thead class="sr-only">
						<tr>
							<th>{i18n.music_track_disc()}</th>
							<th>{i18n.music_track_position()}</th>
							<th>{i18n.music_track_title()}</th>
							<th>{i18n.music_track_length()}</th>
						</tr>
					</thead>
					<tbody>
						{#each tracks as t (t.id)}
							<tr class="border-b border-border/50 last:border-0">
								<td class="w-10 py-1.5 font-mono text-fg-faint">{t.disc}</td>
								<td class="w-10 py-1.5 font-mono text-fg-faint">
									{t.position}
								</td>
								<td class="py-1.5 text-fg">{t.title}</td>
								<td class="w-14 py-1.5 text-right font-mono text-fg-muted">
									{clock(t.duration)}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			{/if}
		</div>
	{/if}
</li>
