<script lang="ts">
	import type { ArrApp, ImportMode, ImportTransferMode } from "@lib/types";
	import RadioCards from "@components/forms/RadioCards.svelte";
	import Select from "@components/forms/Select.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	type Props = {
		app: ArrApp;
		mode: ImportMode;
		importMode: "" | ImportTransferMode;
	};

	let { app, mode = $bindable(), importMode = $bindable() }: Props = $props();

	// The same two cards and transfer modes as the folder form; Sonarr's files
	// land under the series path, Radarr's under the movie path.
	const MODES: { v: ImportMode; label: string; desc: string }[] = $derived([
		{
			v: "in_place",
			label: i18n.imports_adopt_in_place(),
			desc: i18n.imports_adopt_help(),
		},
		{
			v: "rename",
			label: i18n.imports_import_rename(),
			desc:
				app === "sonarr"
					? i18n.imports_rename_desc_series()
					: i18n.imports_rename_desc_movie(),
		},
	]);

	const TRANSFER_MODES: { v: "" | ImportTransferMode; label: string }[] = [
		{ v: "", label: i18n.imports_mode_server_default() },
		{ v: "hardlink", label: i18n.imports_mode_hardlink() },
		{ v: "copy", label: i18n.imports_mode_copy() },
		{ v: "move", label: i18n.imports_mode_move() },
	];
</script>

<div class="space-y-5">
	<div>
		<RadioCards
			legend={i18n.common_mode()}
			columns={2}
			name="arr-mode"
			value={mode}
			onChange={(v) => (mode = v)}
			options={MODES.map((m) => ({
				value: m.v,
				label: m.label,
				description: m.desc,
			}))}
		/>
		<p class="mt-2 text-xs text-fg-muted">
			{mode === "in_place"
				? i18n.arr_mode_rule_in_place()
				: i18n.arr_mode_rule_rename()}
		</p>
	</div>

	{#if mode === "rename"}
		<div>
			<Select
				label={i18n.imports_transfer_mode()}
				value={importMode}
				options={TRANSFER_MODES.map((t) => ({ value: t.v, label: t.label }))}
				onChange={(v) => (importMode = v)}
			/>
			<p class="mt-1 text-xs text-fg-muted">{i18n.imports_overrides_global()}</p>
		</div>
	{/if}
</div>
