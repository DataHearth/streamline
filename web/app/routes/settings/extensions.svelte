<script lang="ts">
	import SkeletonList from "@components/shared/SkeletonList.svelte";
	import { createMutation, createQuery, useQueryClient } from "@tanstack/svelte-query";
	import { ExternalLink, Plus, Puzzle } from "@lucide/svelte";
	import { api, errorText } from "@lib/api";
	import { auth } from "@lib/auth.svelte";
	import { requireAdmin } from "@lib/guards";
	import { toast } from "@lib/toast";
	import {
		ACCOUNT_EXTENSIONS_KEY,
		EXTENSIONS_DOCS_URL,
		EXTENSIONS_KEY,
		EXTENSIONS_PATH,
		extensionPath,
		type Extension,
		type ExtensionList,
	} from "@lib/extensions";
	import ExtensionItem from "@components/settings/extensions/ExtensionItem.svelte";
	import AddExtensionFlow from "@components/settings/extensions/AddExtensionFlow.svelte";
	import EditExtensionModal from "@components/settings/extensions/EditExtensionModal.svelte";
	import ApproveExtensionDialog from "@components/settings/extensions/ApproveExtensionDialog.svelte";
	import DeleteExtensionDialog from "@components/settings/extensions/DeleteExtensionDialog.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Settings → Extensions, admin only: every extension the server knows, in
	// the state it is in, with what each may do. Adding is pasting a source
	// and a checksum from the release notes — no catalogue, by decision.
	// Changes apply live, so nothing here asks for a restart.
	//
	// A read-only instance still opens every form: Add and Edit end on the
	// entry as YAML to paste, and so does Approve. Turning one off and
	// deleting one are edits of the file the admin makes there.
	$effect(() => {
		if (!auth.loading) requireAdmin();
	});

	const qc = useQueryClient();
	const list = createQuery<ExtensionList>(() => ({
		queryKey: EXTENSIONS_KEY,
		queryFn: () => api<ExtensionList>(EXTENSIONS_PATH),
		enabled: !auth.loading && auth.user?.role === "admin",
	}));
	let items = $derived(list.data?.items ?? []);

	let adding = $state(false);
	let editing = $state<Extension | null>(null);
	let approving = $state<Extension | null>(null);
	let deleting = $state<Extension | null>(null);

	const toggle = createMutation<Extension, Error, { name: string; on: boolean }>(() => ({
		mutationFn: ({ name, on }) => api<Extension>(extensionPath(name), { method: "PATCH", body: { enabled: on } }),
		onSuccess: (x, { on }) => {
			qc.invalidateQueries({ queryKey: EXTENSIONS_KEY });
			qc.invalidateQueries({ queryKey: ACCOUNT_EXTENSIONS_KEY });
			toast.ok(on ? i18n.extensions_turned_on({ name: x.name }) : i18n.extensions_turned_off({ name: x.name }));
		},
		onError: (err) => toast.err(errorText(err)),
	}));

	// From the "already installed" answer in the add flow: its Edit is the
	// way to point it at a new release.
	function editByName(name: string) {
		const x = items.find((i) => i.name === name);
		adding = false;
		if (x) editing = x;
	}
</script>

<div class="mx-auto flex max-w-4xl flex-col gap-5">
	<header class="flex flex-wrap items-end justify-between gap-3">
		<p class="min-w-0 max-w-2xl flex-1 basis-72 text-sm leading-relaxed text-fg-muted">{i18n.extensions_intro()}</p>
		<button
			type="button"
			onclick={() => (adding = true)}
			class="inline-flex shrink-0 items-center gap-1.5 rounded-md bg-accent px-3.5 py-2 text-sm font-semibold text-fg-on-accent transition hover:bg-accent-hover"
		>
			<Plus size={16} aria-hidden="true" />
			{i18n.extensions_add()}
		</button>
	</header>

	{#if list.isPending}
		<div class="grid gap-3">
			<SkeletonList variant="card" count={3} />
		</div>
	{:else if list.isError}
		<p class="text-sm text-status-failed">
			{i18n.err_load_failed_detail({ reason: errorText(list.error) })}
		</p>
	{:else if items.length === 0}
		<div class="rounded-lg border border-dashed border-border bg-bg-deep/40 p-8 text-center">
			<Puzzle size={24} class="mx-auto text-fg-faint" aria-hidden="true" />
			<p class="mt-3 text-sm text-fg">{i18n.extensions_none()}</p>
			<p class="mx-auto mt-1 max-w-md text-xs leading-relaxed text-fg-muted">{i18n.extensions_none_help()}</p>
			<a
				href={EXTENSIONS_DOCS_URL}
				target="_blank"
				rel="noopener noreferrer"
				class="touch-hit mt-3 inline-flex items-center gap-1 text-xs font-medium text-accent-text hover:underline"
			>
				{i18n.extensions_docs()}
				<ExternalLink size={12} aria-hidden="true" />
			</a>
		</div>
	{:else}
		<div class="flex flex-col gap-3">
			{#each items as x (x.name)}
				<ExtensionItem
					ext={x}
					busy={toggle.isPending && toggle.variables?.name === x.name}
					onToggle={(on) => toggle.mutate({ name: x.name, on })}
					onEdit={() => (editing = x)}
					onApprove={() => (approving = x)}
					onDelete={() => (deleting = x)}
				/>
			{/each}
		</div>
	{/if}
</div>

<AddExtensionFlow open={adding} onClose={() => (adding = false)} onEditExisting={editByName} />
<EditExtensionModal ext={editing} onClose={() => (editing = null)} />
<ApproveExtensionDialog ext={approving} onClose={() => (approving = null)} />
<DeleteExtensionDialog ext={deleting} onClose={() => (deleting = null)} />
