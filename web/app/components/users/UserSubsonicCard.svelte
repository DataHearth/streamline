<script lang="ts">
	import { createMutation, useQueryClient } from "@tanstack/svelte-query";
	import { api, errorText } from "@lib/api";
	import { toast } from "@lib/toast";
	import type { SubsonicAccess, User } from "@lib/types";
	import Checkbox from "@components/forms/Checkbox.svelte";
	import Select from "@components/forms/Select.svelte";
	import { m as i18n } from "@lib/paraglide/messages.js";

	// Save-per-control: each switch and the bitrate select patch on change.
	let { user }: { user: User } = $props();

	const qc = useQueryClient();

	type Flag = Exclude<keyof SubsonicAccess, "max_bit_rate">;

	const FLAGS: { key: Flag; label: () => string }[] = [
		{ key: "can_stream", label: i18n.users_subsonic_stream },
		{ key: "can_download", label: i18n.users_subsonic_download },
		{ key: "can_manage_playlists", label: i18n.users_subsonic_playlists },
		{ key: "can_share", label: i18n.users_subsonic_share },
		{ key: "can_rate", label: i18n.users_subsonic_rate },
		{ key: "can_change_password", label: i18n.users_subsonic_change_password },
	];

	const RATES = [0, 64, 96, 128, 160, 192, 256, 320];

	// A cap set from elsewhere (an older value, the API) still shows.
	const rateOptions = $derived(
		[...new Set([...RATES, user.subsonic?.max_bit_rate ?? 0])]
			.sort((a, b) => a - b)
			.map((kbps) => ({
				value: String(kbps),
				label:
					kbps === 0
						? i18n.users_subsonic_no_limit()
						: i18n.users_subsonic_kbps({ kbps }),
			})),
	);

	const patch = createMutation<User, Error, Partial<SubsonicAccess>>(() => ({
		mutationFn: (subsonic) =>
			api<User>(`/users/${user.id}`, { method: "PATCH", body: { subsonic } }),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["user", user.id] });
			toast.ok(i18n.common_saved());
		},
		onError: (err) => toast.err(errorText(err)),
	}));
</script>

<section class="rounded-lg border border-border bg-bg-elevated p-5">
	<header class="mb-4">
		<h3 class="text-base font-semibold text-fg">
			{i18n.users_subsonic_permissions()}
		</h3>
		<p class="mt-0.5 text-xs text-fg-muted">{i18n.users_subsonic_help()}</p>
	</header>

	{#if user.subsonic}
		{@const access = user.subsonic}
		<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
			{#each FLAGS as flag (flag.key)}
				<Checkbox
					checked={access[flag.key]}
					label={flag.label()}
					disabled={patch.isPending}
					onChange={(v) => patch.mutate({ [flag.key]: v })}
				/>
			{/each}
		</div>
		<div class="mt-5 max-w-xs">
			<Select
				label={i18n.users_subsonic_max_bitrate()}
				value={String(access.max_bit_rate)}
				options={rateOptions}
				onChange={(v) => patch.mutate({ max_bit_rate: Number(v) })}
			/>
			<p class="mt-1.5 text-xs text-fg-muted">
				{i18n.users_subsonic_max_bitrate_help()}
			</p>
		</div>
	{/if}
</section>
