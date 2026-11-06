<script lang="ts">
	import Dialog from "@components/modals/Dialog.svelte";
	import Checkbox from "@components/forms/Checkbox.svelte";
	import Select from "@components/forms/Select.svelte";
	import AlbumEditionPicker from "@components/music/AlbumEditionPicker.svelte";
	import { createQuery } from "@tanstack/svelte-query";
	import { api } from "@lib/api";
	import { entryHeading, holdFileCount } from "@lib/activity-touch";
	import type {
		FolderPlan,
		HoldAssignments,
		HoldCheck,
		HoldReason,
		QueueEntry,
		TrackPlan,
		TrackPlanTrack,
	} from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// The decision a held download is waiting for. Three outcomes, two buttons:
	// the two destructive ones are one button whose label follows a checkbox,
	// which is how DeleteTitleDialog settled the same problem — two deletes a row
	// apart put the irreversible one a slip of the thumb from the safe one.
	let {
		item,
		pending = false,
		onResolve,
		onClose,
	}: {
		item: QueueEntry | null;
		pending?: boolean;
		onResolve: (
			action: "import" | "regrab" | "delete",
			assignments?: HoldAssignments,
		) => void;
		onClose: () => void;
	} = $props();

	// On by default, and reset on every open: after a bad grab a replacement is
	// almost always what you want, and it is the less final of the two deletes.
	// Never remembered — a box carrying the last download's answer is the one
	// mistake this dialog exists to prevent.
	let regrab = $state(true);
	let openedFor = $state<number | null>(null);
	$effect(() => {
		if (item && item.id !== openedFor) {
			openedFor = item.id;
			regrab = true;
		}
		if (!item) openedFor = null;
	});

	// A discography pack has no plan and answers 409: the dialog then keeps its
	// plain "Import anyway".
	const heldFiles = $derived(
		new Set(
			(item?.hold_reasons ?? [])
				.filter((r) => r.check === "track")
				.map((r) => r.file),
		),
	);
	const needsPlan = $derived(
		heldFiles.size > 0 ||
			(item?.hold_reasons ?? []).some((r) => r.check === "missing"),
	);
	const planQuery = createQuery<TrackPlan>(() => ({
		queryKey: ["track-plan", item?.id],
		queryFn: () => api<TrackPlan>(`/downloads/${item?.id}/track-plan`),
		enabled: needsPlan,
		retry: false,
		gcTime: 0,
		refetchOnWindowFocus: false,
	}));
	const plan = $derived(needsPlan ? planQuery.data : undefined);

	const heldFolders = $derived(
		new Set(
			(item?.hold_reasons ?? [])
				.filter((r) => r.check === "folder")
				.map((r) => r.file),
		),
	);
	const folderQuery = createQuery<FolderPlan>(() => ({
		queryKey: ["folder-plan", item?.id],
		queryFn: () => api<FolderPlan>(`/downloads/${item?.id}/folder-plan`),
		enabled: heldFolders.size > 0,
		retry: false,
		gcTime: 0,
		refetchOnWindowFocus: false,
	}));
	const folderPlan = $derived(heldFolders.size > 0 ? folderQuery.data : undefined);

	// The chosen album per folder, "" for a folder left out.
	let folderChoices = $state<Record<string, string>>({});
	let foldersSeededFor = $state<number | null>(null);
	$effect(() => {
		if (!item) foldersSeededFor = null;
		if (item && folderPlan && foldersSeededFor !== item.id) {
			foldersSeededFor = item.id;
			folderChoices = Object.fromEntries(
				folderPlan.folders.map((f) => [
					f.folder,
					f.album_id ? String(f.album_id) : "",
				]),
			);
		}
	});
	const planFolders = $derived(
		(folderPlan?.folders ?? []).toSorted(
			(a, b) => Number(heldFolders.has(b.folder)) - Number(heldFolders.has(a.folder)),
		),
	);
	const chosenAlbums = $derived(Object.values(folderChoices).filter(Boolean));
	const albumTaken = (album: string) =>
		album !== "" && chosenAlbums.filter((c) => c === album).length > 1;
	const albumOptions = $derived([
		{ value: "", label: i18n.hold_track_skip() },
		...(folderPlan?.albums ?? []).map((a) => ({
			value: String(a.id),
			label: a.year ? `${a.year} · ${a.title}` : a.title,
		})),
	]);
	const foldersReady = $derived(
		!folderPlan ||
			(chosenAlbums.length > 0 && new Set(chosenAlbums).size === chosenAlbums.length),
	);

	// The chosen track per file, "" for a file left out.
	let choices = $state<Record<string, string>>({});
	let seededFor = $state<number | null>(null);
	$effect(() => {
		if (!item) seededFor = null;
		if (item && plan && seededFor !== item.id) {
			seededFor = item.id;
			choices = Object.fromEntries(
				plan.files.map((f) => [f.file, f.track_id ? String(f.track_id) : ""]),
			);
		}
	});

	const planFiles = $derived(
		(plan?.files ?? []).toSorted(
			(a, b) => Number(heldFiles.has(b.file)) - Number(heldFiles.has(a.file)),
		),
	);
	const chosen = $derived(Object.values(choices).filter(Boolean));
	const taken = (track: string) =>
		track !== "" && chosen.filter((c) => c === track).length > 1;
	const canImport = $derived(
		!plan || (chosen.length > 0 && new Set(chosen).size === chosen.length),
	);

	const emptyTracks = $derived(
		(plan?.tracks ?? []).filter(
			(t) => !t.has_file && !chosen.includes(String(t.id)),
		),
	);

	const trackLabel = (t: TrackPlanTrack, tracks: TrackPlanTrack[]) => {
		const number = String(t.position).padStart(2, "0");
		return tracks.some((o) => o.disc > 1)
			? `${t.disc}-${number} · ${t.title}`
			: `${number} · ${t.title}`;
	};
	const trackOptions = $derived([
		{ value: "", label: i18n.hold_track_skip() },
		...(plan?.tracks ?? []).map((t) => ({
			value: String(t.id),
			label: trackLabel(t, plan?.tracks ?? []),
		})),
	]);

	function importHeld() {
		onResolve("import", {
			track_assignments:
				plan &&
				Object.entries(choices)
					.filter(([, track]) => track)
					.map(([file, track]) => ({ file, track_id: Number(track) })),
			folder_assignments:
				folderPlan &&
				Object.entries(folderChoices)
					.filter(([, album]) => album)
					.map(([folder, album]) => ({ folder, album_id: Number(album) })),
		});
	}

	const CHECK_LABELS: Record<HoldCheck, () => string> = {
		resolution: () => i18n.hold_check_resolution(),
		duration: () => i18n.hold_check_duration(),
		codec: () => i18n.hold_check_codec(),
		corrupt: () => i18n.hold_check_corrupt(),
		always_ask: () => i18n.hold_check_always_ask(),
		tier: () => i18n.hold_check_tier(),
		album: () => i18n.hold_check_album(),
		track: () => i18n.hold_check_track(),
		missing: () => i18n.hold_check_missing(),
		folder: () => i18n.hold_check_folder(),
		format: () => i18n.hold_check_format(),
		bitrate: () => i18n.hold_check_bitrate(),
		metadata: () => i18n.hold_check_metadata(),
	};

	// `always_ask` holds a file nothing is wrong with, so it has no claim to
	// contrast against — rendering the pair would read as a fifth failure.
	const hasExpectation = (r: HoldReason) => r.check !== "always_ask";

	// The hold is on the download; the reasons are itemised per file. A season
	// pack therefore lists several files, each with its own findings.
	type FileGroup = { file: string; name: string; findings: HoldReason[] };
	let groups = $derived.by<FileGroup[]>(() => {
		const out = new Map<string, FileGroup>();
		for (const r of item?.hold_reasons ?? []) {
			if (
				r.check === "missing" ||
				(plan && r.check === "track") ||
				(folderPlan && r.check === "folder")
			)
				continue;
			let g = out.get(r.file);
			if (!g) {
				g = {
					file: r.file,
					// The prefix is the same for every file in a pack, so the name is
					// what tells them apart. Full path stays in the title.
					name: r.file.split("/").pop() || r.file,
					findings: [],
				};
				out.set(r.file, g);
			}
			g.findings.push(r);
		}
		return [...out.values()];
	});

	let fileCount = $derived(holdFileCount(item?.hold_reasons));
</script>

<Dialog
	open={item !== null}
	title={item ? entryHeading(item) : ""}
	size="lg"
	{onClose}
	actions={[
		{ label: i18n.common_cancel(), variant: "ghost", autofocus: true },
		{
			label: plan || folderPlan ? i18n.hold_import() : i18n.hold_import_anyway(),
			variant: "ghost",
			dismiss: false,
			disabled:
				pending ||
				!canImport ||
				!foldersReady ||
				(needsPlan && planQuery.isPending) ||
				(heldFolders.size > 0 && folderQuery.isPending),
			onClick: importHeld,
		},
		{
			label: regrab ? i18n.hold_delete_and_search() : i18n.common_delete(),
			variant: "danger",
			dismiss: false,
			pending,
			onClick: () => onResolve(regrab ? "regrab" : "delete"),
		},
	]}
>
	<p class="text-sm leading-relaxed text-fg-muted">
		{heldFolders.size > 0
			? i18n.hold_body_folders()
			: fileCount === 0
			? i18n.hold_body_incomplete()
			: fileCount === 1
				? i18n.hold_body_one()
				: i18n.hold_body_other({ count: fileCount })}
	</p>

	<div class="mt-4 flex flex-col gap-3">
		{#each groups as g (g.file)}
			<div class="overflow-hidden rounded-md border border-border">
				<div
					class="truncate border-b border-border bg-bg px-2.5 py-2 font-mono text-[11px] text-fg"
					title={g.file}
				>
					{g.name}
				</div>
				<dl class="divide-y divide-border">
					{#each g.findings as f (f.check)}
						<div
							class="grid grid-cols-[auto_1fr] items-baseline gap-x-3 px-2.5 py-2"
						>
							<dt
								class="font-mono text-[10px] uppercase tracking-[0.1em] text-fg-faint"
							>
								{CHECK_LABELS[f.check]?.() ?? f.check}
							</dt>
							<dd
								class="flex items-baseline justify-end gap-2 text-right font-mono text-xs"
							>
								{#if hasExpectation(f)}
									{#if f.expected}
										<span class="text-fg-subtle line-through">{f.expected}</span>
										<span class="text-fg-faint" aria-hidden="true">→</span>
									{/if}
									<span style:color="var(--status-held)">{f.actual || "—"}</span>
								{/if}
							</dd>
						</div>
					{/each}
				</dl>
			</div>
		{/each}
	</div>

	{#if folderPlan}
		<p class="mt-4 text-sm text-fg-muted">{i18n.hold_folder_help()}</p>
		<ul class="mt-2 flex max-h-80 flex-col gap-2 overflow-y-auto">
			{#each planFolders as f (f.folder)}
				<li class="grid gap-1 sm:grid-cols-2 sm:items-center sm:gap-3">
					<span
						class="truncate font-mono text-xs"
						class:text-fg={!heldFolders.has(f.folder)}
						style:color={heldFolders.has(f.folder) ? "var(--status-held)" : undefined}
						title={f.folder}
					>
						{f.folder.split("/").pop()}
					</span>
					<div>
						<Select
							ariaLabel={f.folder.split("/").pop()}
							value={folderChoices[f.folder] ?? ""}
							options={albumOptions}
							onChange={(v) => (folderChoices[f.folder] = v)}
							searchable={albumOptions.length > 10}
							maxHeight={240}
						/>
						{#if albumTaken(folderChoices[f.folder] ?? "")}
							<p class="mt-1 text-xs text-status-held">{i18n.hold_folder_taken()}</p>
						{/if}
					</div>
				</li>
			{/each}
		</ul>
	{/if}

	{#if plan}
		{#if item?.album}
			<div class="mt-4">
				{#key item.album.id}
					<AlbumEditionPicker
						albumId={item.album.id}
						onChanged={() => {
							seededFor = null;
							planQuery.refetch();
						}}
					/>
				{/key}
			</div>
		{/if}
		<p class="mt-4 text-sm text-fg-muted">{i18n.hold_track_help()}</p>
		<ul class="mt-2 flex max-h-80 flex-col gap-2 overflow-y-auto">
			{#each planFiles as f (f.file)}
				<li class="grid gap-1 sm:grid-cols-2 sm:items-center sm:gap-3">
					<span
						class="truncate font-mono text-xs"
						class:text-fg={!heldFiles.has(f.file)}
						style:color={heldFiles.has(f.file) ? "var(--status-held)" : undefined}
						title={f.file}
					>
						{f.file.split("/").pop()}
					</span>
					<div>
						<Select
							ariaLabel={f.file.split("/").pop()}
							value={choices[f.file] ?? ""}
							options={trackOptions}
							onChange={(v) => (choices[f.file] = v)}
							searchable={trackOptions.length > 10}
							maxHeight={240}
						/>
						{#if taken(choices[f.file] ?? "")}
							<p class="mt-1 text-xs text-status-held">{i18n.hold_track_taken()}</p>
						{/if}
					</div>
				</li>
			{/each}
		</ul>
		{#if emptyTracks.length > 0}
			<p class="mt-3 text-xs text-fg-muted">
				{i18n.hold_tracks_empty({
					titles: emptyTracks.map((t) => t.title).join(" · "),
				})}
			</p>
		{/if}
	{/if}

	<div class="mt-4 border-t border-border pt-3.5">
		<Checkbox
			checked={regrab}
			onChange={(v) => (regrab = v)}
			label={i18n.hold_search_replacement()}
			description={i18n.hold_search_replacement_help()}
		/>
	</div>
</Dialog>
