<script lang="ts">
	import BottomSheet from "@components/shared/BottomSheet.svelte";
	import { Check, RotateCcw, TriangleAlert, X } from "@lucide/svelte";
	import { formatRelative } from "@lib/dates";
	import {
		STATUS_META,
		isMusicBook,
		albumBy,
		requestedAs,
		kindLabel,
		requesterName,
	} from "@lib/requests-touch";
	import Select from "@components/forms/Select.svelte";
	import LookupDetailPanel from "@components/shared/LookupDetailPanel.svelte";
	import MusicBookLookupPanel from "@components/shared/MusicBookLookupPanel.svelte";
	import { auth } from "@lib/auth.svelte";
	import {
		hardcoverCode,
		hardcoverFix,
		hardcoverTitle,
		requestHit,
		type ArtistMeta,
		type BookMeta,
		type RequestMetadata,
	} from "@lib/music-books-lookup";
	import type {
		MediaRequest,
		QualityProfile,
		RequestMediaDetails,
	} from "@lib/types";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// B1: one surface can change a request. The row says what needs deciding;
	// this says everything you need to decide it and holds both decisions, so
	// the quality override is always in front of you when you commit.
	let {
		request,
		detail,
		detailLoading = false,
		detailError = false,
		reviewer,
		profiles = [],
		profile,
		onProfileChange,
		approveError = null,
		busy = false,
		onClose,
		onApprove,
		onReject,
		onReopen,
	}: {
		request: MediaRequest | null;
		detail?: RequestMetadata;
		detailLoading?: boolean;
		detailError?: boolean;
		reviewer: boolean;
		profiles?: QualityProfile[];
		profile: string;
		onProfileChange: (v: string) => void;
		// The code the last approval of this request failed with, if it did.
		approveError?: string | null;
		busy?: boolean;
		onClose: () => void;
		onApprove: (r: MediaRequest, profile: string) => void;
		onReject: (r: MediaRequest) => void;
		onReopen: (r: MediaRequest) => void;
	} = $props();

	// Mounted with the page; open while it renders a request. A watcher armed while closed
	// would take the phone's Back from the page.

	let meta = $derived(request ? STATUS_META[request.status] : null);
	// Music and book requests carry MusicBrainz or Hardcover metadata, so they get
	// that panel; the detail is only ever one shape at a time.
	let mb = $derived(request ? isMusicBook(request.media_type) : false);
	let mbMeta = $derived(mb ? (detail as ArtistMeta | BookMeta | undefined) : undefined);
	let mbHit = $derived(request && mb ? requestHit(request, mbMeta) : undefined);
	// Only the detail that belongs to this request, never a cached neighbour's.
	let mbDetail = $derived(mbHit && (mbHit.artist || mbHit.book) ? mbMeta : undefined);
	let videoDetail = $derived(mb ? undefined : (detail as RequestMediaDetails | undefined));
	// An album request is reviewed on its artist, whose year is when the artist
	// started; the album's own year is on the discography, when it is there.
	let year = $derived(
		!mb
			? videoDetail?.year
			: request?.media_type === "album"
				? (mbDetail as ArtistMeta | undefined)?.releases?.find((x) => x.mbid === request?.media_mbid)?.year
				: mbHit?.year,
	);
	let pending = $derived(request?.status === "pending");
	// An album whose artist is already in the library joins that artist under
	// its profile, so there is no profile to pick.
	// The artist approval will act on, from the metadata and not from what the
	// requester typed.
	let verifiedArtist = $derived(mbHit?.artist?.name ?? request?.artist_name ?? "");
	let profileLocked = $derived(request?.media_type === "album" && !!mbHit?.held);
	let notFound = $derived(approveError === "album_not_found");
	// Hardcover refusing the add that approving a book or a series makes.
	let keyIssue = $derived(hardcoverCode(approveError));
	let options = $derived([
		{ value: "", label: i18n.quality_server_default() },
		...profiles.map((p) => ({ value: p.name, label: p.name })),
	]);
</script>

{#if request && meta}
	{#snippet sheetFooter()}
					{#if pending}
						<button
							type="button"
							disabled={busy}
							onclick={() => onReject(request)}
							class="inline-flex h-11 flex-1 items-center justify-center gap-2 rounded-xl border border-border bg-surface text-[14px] font-medium text-fg transition active:bg-surface-2 disabled:opacity-60"
						>
							<X size={15} aria-hidden="true" />
							{i18n.common_reject()}
						</button>
						<button
							type="button"
							disabled={busy}
							onclick={() => onApprove(request, profile)}
							class="inline-flex h-11 flex-[1.4] items-center justify-center gap-2 rounded-xl bg-accent text-[14px] font-semibold text-fg-on-accent transition active:bg-accent-pressed disabled:opacity-60"
						>
							<Check size={15} aria-hidden="true" />
							{i18n.requests_approve_add()}
						</button>
					{:else}
						<button
							type="button"
							disabled={busy}
							onclick={() => onReopen(request)}
							class="inline-flex h-11 flex-1 items-center justify-center gap-2 rounded-xl border border-border bg-surface text-[14px] font-medium text-fg transition active:bg-surface-2 disabled:opacity-60"
						>
							<RotateCcw size={15} aria-hidden="true" />
							{i18n.requests_reopen()}
						</button>
					{/if}
	{/snippet}

	<BottomSheet
		label={i18n.requests_detail()}
		{onClose}
		headerClass="items-start border-b border-border pb-3.5"
		bodyClass="pb-3 pt-4"
		footer={reviewer ? sheetFooter : undefined}
	>
		{#snippet header()}
			<div class="min-w-0 pt-1">
				<h2 class="truncate text-[17px] font-semibold tracking-tight text-fg">
					{request.title}
				</h2>
				<p class="mt-1 font-mono text-[11px] text-fg-subtle">
					{year ? `${year} · ` : ""}{kindLabel(
						request.media_type,
					)}{albumBy(request, verifiedArtist) ? ` · ${albumBy(request, verifiedArtist)}` : ""}
				</p>
				{#if requestedAs(request, verifiedArtist)}
					<p class="mt-0.5 truncate text-[11.5px] text-fg-subtle">{requestedAs(request, verifiedArtist)}</p>
				{/if}
			</div>
		{/snippet}
		<!-- What was asked, by whom, and where it landed. This is what the sheet
		     was opened to answer, so it leads; the artwork and synopsis below are
		     for judging the title itself. The decision sits at the head of the
		     list rather than as a pill in the header — for a rejection it reads
		     directly into the reason under it. -->
		<div class="pb-1">
			<div class="flex items-baseline justify-between gap-4 py-2 text-[13px]">
				<span class="shrink-0 text-fg-subtle">{i18n.common_decision()}</span>
				<span
					class="min-w-0 truncate text-right font-semibold"
					style:color="var(--status-{meta.token})"
				>
					{meta.label}
				</span>
			</div>

			<!-- The reason belongs to the decision, so it hangs directly off it
			     rather than sitting below the rest of the metadata. -->
			{#if request.status === "denied" && request.reason}
				<blockquote
					class="mb-1 border-l-2 border-status-failed/50 pl-3 text-[13px] leading-relaxed text-fg-muted"
				>
					{request.reason}
				</blockquote>
			{/if}

			{#each [{ k: i18n.requests_requested_by(), v: requesterName(request) }, { k: i18n.common_when(), v: formatRelative(request.created_at) }, { k: i18n.requests_asked_for(), v: request.quality_profile || i18n.quality_no_preference(), mono: true }, ...(request.approved_by ? [{ k: i18n.requests_decided_by(), v: `${request.approved_by.display_name || request.approved_by.email} · ${formatRelative(request.updated_at)}` }] : [])] as row (row.k)}
				<div class="flex items-baseline justify-between gap-4 py-2 text-[13px]">
					<span class="shrink-0 text-fg-subtle">{row.k}</span>
					<span
						class="min-w-0 truncate text-right text-fg {row.mono
							? 'font-mono text-[12px]'
							: ''}"
					>
						{row.v}
					</span>
				</div>
			{/each}
		</div>

		<div class="mt-3 border-t border-border pt-4">
			{#if detailError}
				<p class="text-[13px] text-fg-subtle">{i18n.requests_load_failed()}</p>
			{:else if mbHit}
				<MusicBookLookupPanel
					kind={mbHit.kind}
					hit={mbHit}
					detail={mbDetail}
					loading={detailLoading}
					showTitle={false}
					compact
					markedAlbum={request.media_type === "album" ? request.media_mbid : undefined}
				/>
				{#if request.media_type === "album" && verifiedArtist}
					<p class="mt-3 text-[12px] text-fg-muted">
						{mbHit.held
							? i18n.requests_album_note_held({ artist: verifiedArtist })
							: i18n.requests_album_note_new({ artist: verifiedArtist })}
					</p>
				{/if}
			{:else}
				<LookupDetailPanel
					kind={request.media_type === "tvshow" ? "series" : "movie"}
					item={{
						title: request.title,
						year: videoDetail?.year,
						poster_url: videoDetail?.poster_url,
						overview: videoDetail?.overview,
					}}
					detail={videoDetail}
					loading={detailLoading}
					showTitle={false}
					compact
				/>
			{/if}
		</div>

		{#if reviewer && pending}
			<div class="mt-3 border-t border-border pt-4">
				<div
					class="mb-2 eyebrow text-[9.5px] text-fg-subtle"
				>
					{i18n.requests_add_with_profile()}
				</div>
				<Select
					id="qp-sheet-{request.id}"
					value={profile}
					{options}
					disabled={profileLocked}
					onChange={onProfileChange}
				/>
				{#if profileLocked && verifiedArtist}
					<p class="mt-1.5 text-[12px] text-fg-muted">
						{i18n.requests_album_profile_held({ artist: verifiedArtist })}
					</p>
				{/if}
			</div>
		{/if}
		{#snippet pinned()}
			<!-- Pinned with the buttons, not in the scroll: it answers the tap that
			     was just made, wherever the sheet was scrolled to. -->
			{#if reviewer && pending && (notFound || keyIssue)}
				<div
					role="alert"
					class="flex items-start gap-2.5 border-t border-status-failed/30 bg-status-failed/10 px-5 py-3 text-status-failed"
				>
					<TriangleAlert size={15} class="mt-0.5 shrink-0" aria-hidden="true" />
					<div class="min-w-0">
						<p class="text-[13px] font-semibold">
							{keyIssue ? hardcoverTitle(keyIssue) : i18n.requests_album_not_found()}
						</p>
						<p class="mt-0.5 text-[12px] leading-relaxed text-fg-muted">
							{keyIssue
								? hardcoverFix(keyIssue, auth.user?.role === "admin")
								: i18n.requests_album_not_found_body({
										album: request.title,
										artist: verifiedArtist,
										requester: requesterName(request),
									})}
						</p>
					</div>
				</div>
			{/if}
		{/snippet}
	</BottomSheet>
{/if}
