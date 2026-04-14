import type { StatusKind } from "../components/shared/StatusPill.svelte";

// formatBytes renders a byte count as a compact human string ("4.1 GB").
// Returns an em dash when unknown so columns stay aligned.
export function formatBytes(n: number | undefined | null): string {
	if (!n || n <= 0) return "—";
	const units = ["B", "KB", "MB", "GB", "TB"];
	let v = n;
	let i = 0;
	while (v >= 1024 && i < units.length - 1) {
		v /= 1024;
		i++;
	}
	return `${i > 0 && v < 10 ? v.toFixed(1) : Math.round(v)} ${units[i]}`;
}

// formatSpeed renders a bytes/sec rate ("1.2 MB/s"); empty when idle.
export function formatSpeed(bytesPerSec: number | undefined | null): string {
	if (!bytesPerSec || bytesPerSec <= 0) return "";
	return `${formatBytes(bytesPerSec)}/s`;
}

// formatEta renders seconds-to-completion ("2m", "1h 04m"); empty when
// unknown (the backend already normalizes qBittorrent's ∞ sentinel to 0).
export function formatEta(seconds: number | undefined | null): string {
	if (!seconds || seconds <= 0) return "";
	const s = Math.round(seconds);
	if (s < 60) return `${s}s`;
	const m = Math.floor(s / 60);
	if (m < 60) return `${m}m`;
	const h = Math.floor(m / 60);
	return `${h}h ${String(m % 60).padStart(2, "0")}m`;
}

// pillStatus maps a download lifecycle status onto an existing StatusPill
// kind/token (no shared-component change): importing reuses the "grabbing"
// in-progress token, error/failed share "failed", completed → "available".
export function pillStatus(
	status:
		| "downloading"
		| "importing"
		| "paused"
		| "error"
		| "completed"
		| "failed",
): StatusKind {
	switch (status) {
		case "importing":
			return "grabbing";
		case "error":
		case "failed":
			return "failed";
		case "completed":
			return "available";
		case "paused":
			return "paused";
		default:
			return "downloading";
	}
}
