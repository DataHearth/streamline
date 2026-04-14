<script lang="ts">
	let { email = "", name = "", size = 32 } = $props();

	function emailHue(e) {
		let h = 0;
		for (let i = 0; i < e.length; i++) {
			h = ((h << 5) - h + e.charCodeAt(i)) | 0;
		}
		return Math.abs(h) % 360;
	}

	function computeInitials(n, e) {
		const source = (n?.trim() || e || "").split(/[\s@]+/).filter(Boolean);
		const first = source[0]?.[0] ?? "";
		const second = source[1]?.[0] ?? "";
		return (first + second).toUpperCase();
	}

	let hue = $derived(emailHue(email));
	let initials = $derived(computeInitials(name, email));
	let gradient = $derived(
		`linear-gradient(135deg, hsl(${hue} 75% 70%), hsl(${(hue + 40) % 360} 70% 65%))`,
	);
</script>

<span
	class="avatar inline-grid place-items-center rounded-md font-semibold text-bg-deep"
	style:--avatar-size="{size}px"
	style:--avatar-font="{size * 0.4}px"
	style:--avatar-bg={gradient}
>
	{initials}
</span>

<style>
	.avatar {
		width: var(--avatar-size);
		height: var(--avatar-size);
		font-size: var(--avatar-font);
		background: var(--avatar-bg);
	}
</style>
