<script lang="ts">
	import {
		createMutation,
		createQuery,
		useQueryClient,
	} from "@tanstack/svelte-query";
	import { goto, params } from "@roxi/routify";
	import { onMount } from "svelte";
	import { ArrowLeft, Bookmark, RefreshCw, Trash2 } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { auth } from "@lib/auth.svelte";
	import { cn } from "@lib/cn";
	import { initials } from "@lib/people";
	import { toast } from "@lib/toast";
	import AlbumRow from "@components/music/AlbumRow.svelte";
	import Select from "@components/forms/Select.svelte";
	import DeleteTitleDialog from "@components/shared/DeleteTitleDialog.svelte";
	import Skeleton from "@components/shared/Skeleton.svelte";
	import type {
		MusicAlbum,
		MusicAlbumType,
		MusicArtist,
		MusicQualityProfile,
	} from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	const TYPE_ORDER: MusicAlbumType[] = [
		"album",
		"ep",
		"single",
		"compilation",
		"live",
		"other",
	];

	function typeLabel(t: MusicAlbumType): string {
		switch (t) {
			case "album":
				return i18n.music_type_album();
			case "ep":
				return i18n.music_type_ep();
			case "single":
				return i18n.music_type_single();
			case "compilation":
				return i18n.music_type_compilation();
			case "live":
				return i18n.music_type_live();
			case "other":
				return i18n.music_type_other();
			default: {
				const unhandled: never = t;
				return unhandled;
			}
		}
	}

	let routeParams = $state<Record<string, string>>({});
	let navigate = $state<(path: string) => void>(() => {});
	let expanded = $state(new Set<number>());
	let deleteOpen = $state(false);

	onMount(() => {
		// Routify reuses this instance across /music/[id] param changes, so the
		// per-artist UI state is cleared by hand. The first emission only records
		// the id.
		let currentId: string | undefined;
		const u1 = params.subscribe((p) => {
			if (currentId !== undefined && p.id !== currentId) {
				expanded = new Set();
				deleteOpen = false;
			}
			currentId = p.id;
			routeParams = p;
		});
		const u2 = goto.subscribe((fn) => (navigate = fn));
		return () => {
			u1();
			u2();
		};
	});
	const artistId = $derived(Number(routeParams.id));

	const qc = useQueryClient();

	const artistQuery = createQuery<MusicArtist>(() => ({
		queryKey: ["music", "artist", artistId],
		queryFn: () => api<MusicArtist>(`/music/artists/${artistId}`),
		enabled: Number.isFinite(artistId) && artistId > 0,
	}));
	let artist = $derived(artistQuery.data);

	const profiles = createQuery<MusicQualityProfile[]>(() => ({
		queryKey: ["music", "quality-profiles"],
		queryFn: () => api<MusicQualityProfile[]>("/music/quality-profiles"),
	}));
	let profileOptions = $derived([
		{ value: "", label: i18n.quality_server_default() },
		...(profiles.data ?? []).map((p) => ({ value: p.name, label: p.name })),
	]);

	let groups = $derived(
		TYPE_ORDER.map((type) => ({
			type,
			albums: (artist?.albums ?? [])
				.filter((a) => a.type === type)
				.toSorted((a, b) =>
					(a.release_date ?? "9999").localeCompare(b.release_date ?? "9999"),
				),
		})).filter((g) => g.albums.length > 0),
	);

	function invalidate() {
		qc.invalidateQueries({ queryKey: ["music"] });
	}

	const patch = createMutation<
		MusicArtist,
		Error,
		{ monitored?: boolean; quality_profile?: string }
	>(() => ({
		mutationFn: (body) =>
			api<MusicArtist>(`/music/artists/${artistId}`, {
				method: "PATCH",
				body,
			}),
		onSuccess: () => invalidate(),
		onError: (e) => toast.err(errorText(e, i18n.common_update_failed())),
	}));

	const refresh = createMutation<MusicArtist, Error>(() => ({
		mutationFn: () =>
			api<MusicArtist>(`/music/artists/${artistId}/refresh`, {
				method: "POST",
			}),
		onSuccess: () => {
			invalidate();
			toast.ok(i18n.music_refreshed());
		},
		onError: (e) => toast.err(errorText(e, i18n.common_refresh_failed())),
	}));

	const del = createMutation<unknown, Error, boolean>(() => ({
		mutationFn: (withFiles) =>
			api(`/music/artists/${artistId}?delete_files=${withFiles}`, {
				method: "DELETE",
			}),
		onSuccess: () => {
			invalidate();
			toast.ok(i18n.music_deleted());
			navigate("/music");
		},
		onError: (e) => toast.err(errorText(e, i18n.common_delete_failed())),
	}));

	function toggle(album: MusicAlbum) {
		const next = new Set(expanded);
		if (!next.delete(album.id)) next.add(album.id);
		expanded = next;
	}
</script>

<div class="flex flex-col gap-6 px-4 py-4 md:px-8">
	<a
		href="/music"
		class="inline-flex w-fit items-center gap-1.5 text-xs font-medium text-fg-muted transition hover:text-fg"
	>
		<ArrowLeft size={13} aria-hidden="true" />
		{i18n.music_label()}
	</a>

	{#if artistQuery.isPending}
		<span class="sr-only" role="status">{i18n.common_loading()}</span>
		<div class="flex items-center gap-5">
			<Skeleton w="96px" h={96} round="full" />
			<div class="flex flex-1 flex-col gap-2">
				<Skeleton w="40%" h={28} />
				<Skeleton w="70%" h={12} />
			</div>
		</div>
		<Skeleton h={64} round="md" />
		<Skeleton h={64} round="md" />
	{:else if artistQuery.isError}
		<div
			class="rounded-lg border border-dashed border-status-failed/40 bg-status-failed/5 py-12 text-center"
		>
			<p class="text-sm font-semibold text-status-failed">
				{i18n.music_load_failed()}
			</p>
			<p class="mt-1 text-xs text-fg-subtle">
				{errorText(artistQuery.error, i18n.common_unknown_error())}
			</p>
		</div>
	{:else if artist}
		<header class="flex flex-col gap-5 md:flex-row md:items-start">
			<span
				class="grid h-24 w-24 shrink-0 place-items-center rounded-full bg-bg-card text-3xl font-semibold text-fg-muted"
				aria-hidden="true"
			>
				{initials(artist.name)}
			</span>
			<div class="min-w-0 flex-1">
				<h1
					class="text-2xl font-bold leading-tight tracking-tight text-fg md:text-4xl"
				>
					{artist.name}
				</h1>
				{#if artist.overview}
					<p
						class="mt-3 line-clamp-4 max-w-3xl text-sm leading-relaxed text-fg-muted [text-wrap:pretty]"
					>
						{artist.overview}
					</p>
				{/if}

				{#if auth.canAddDirectly}
					<div class="mt-4 flex flex-wrap items-center gap-2.5">
						<button
							type="button"
							onclick={() => patch.mutate({ monitored: !artist.monitored })}
							disabled={patch.isPending}
							aria-pressed={artist.monitored}
							class={cn(
								"inline-flex h-10 items-center gap-2 rounded-md border px-3.5 text-sm font-medium transition disabled:opacity-60",
								artist.monitored
									? "border-accent-line bg-accent-soft text-accent-text"
									: "border-border-strong text-fg hover:bg-white/[0.06]",
							)}
						>
							<Bookmark
								size={15}
								fill={artist.monitored ? "currentColor" : "none"}
								aria-hidden="true"
							/>
							{artist.monitored
								? i18n.monitor_monitored()
								: i18n.action_monitor()}
						</button>

						<div class="w-52">
							<Select
								ariaLabel={i18n.quality_profile()}
								value={artist.quality_profile}
								options={profileOptions}
								disabled={patch.isPending}
								onChange={(v) => patch.mutate({ quality_profile: v })}
							/>
						</div>

						<button
							type="button"
							onclick={() => refresh.mutate()}
							disabled={refresh.isPending}
							class="inline-flex h-10 items-center gap-2 rounded-md border border-border-strong px-3.5 text-sm font-medium text-fg transition hover:bg-white/[0.06] disabled:opacity-60"
						>
							<RefreshCw
								size={15}
								class={cn(refresh.isPending && "animate-spin")}
								aria-hidden="true"
							/>
							{refresh.isPending
								? i18n.common_refreshing()
								: i18n.action_refresh_metadata()}
						</button>

						<button
							type="button"
							onclick={() => (deleteOpen = true)}
							class="inline-flex h-10 items-center gap-2 rounded-md border border-border-strong px-3.5 text-sm font-medium text-status-failed transition hover:bg-status-failed/10"
						>
							<Trash2 size={15} aria-hidden="true" />
							{i18n.common_delete()}
						</button>
					</div>
				{/if}
			</div>
		</header>

		{#if groups.length === 0}
			<p class="py-8 text-center text-sm text-fg-subtle">
				{i18n.music_no_albums()}
			</p>
		{:else}
			{#each groups as group (group.type)}
				<section aria-labelledby="music-group-{group.type}">
					<h2
						id="music-group-{group.type}"
						class="mb-2 font-mono text-[11px] uppercase tracking-[0.14em] text-fg-faint"
					>
						{typeLabel(group.type)}
						<span class="ml-1 text-fg-subtle">{group.albums.length}</span>
					</h2>
					<ul class="flex flex-col gap-2">
						{#each group.albums as album (album.id)}
							<AlbumRow
								{album}
								open={expanded.has(album.id)}
								onToggle={() => toggle(album)}
							/>
						{/each}
					</ul>
				</section>
			{/each}
		{/if}

		<DeleteTitleDialog
			open={deleteOpen}
			title={i18n.music_remove_title({ name: artist.name })}
			body={i18n.music_remove_body()}
			filesLabel={i18n.music_delete_files_label()}
			filesNote={i18n.common_cannot_undo()}
			pending={del.isPending}
			onClose={() => (deleteOpen = false)}
			onConfirm={(withFiles) => del.mutate(withFiles)}
		/>
	{/if}
</div>
