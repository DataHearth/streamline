import type { Schedule } from "./types";
import { m as i18n } from "./paraglide/messages.js";

export type ScheduleStateKey =
	| "failed"
	| "running"
	| "paused"
	| "ok"
	| "skipped"
	| "never";

export type ScheduleState = {
	key: ScheduleStateKey;
	label: string;
	// Tailwind text colour for the row's trailing word and the sheet's pill.
	tone: string;
};

export function scheduleState(s: Schedule): ScheduleState {
	if (s.running)
		return {
			key: "running",
			label: i18n.common_running_ellipsis(),
			tone: "text-accent-text",
		};
	if (s.paused)
		return { key: "paused", label: i18n.status_paused(), tone: "text-fg-muted" };
	switch (s.status) {
		case "success":
			return {
				key: "ok",
				label: i18n.common_ok(),
				tone: "text-status-available",
			};
		case "error":
			return {
				key: "failed",
				label: i18n.status_failed(),
				tone: "text-status-failed",
			};
		case "skipped":
			return {
				key: "skipped",
				// "Skipped" on its own reads as a question — skipped what? The row's
				// other two facts are the interval and the last run, so name the run.
				label: i18n.schedule_skipped_run(),
				tone: "text-fg-muted",
			};
		default:
			return { key: "never", label: i18n.common_never(), tone: "text-fg-muted" };
	}
}

export type ScheduleGroupKey = "running" | "scheduled" | "system";

export type ScheduleGroup = {
	key: ScheduleGroupKey;
	label: string;
	rows: Schedule[];
	// Only the interesting group states its size; "Scheduled: 9" is noise.
	count: boolean;
};

// Twelve jobs sorted by name is a list with no shape; grouping by state gives
// one. Running first because it is the only thing currently happening, then
// everything on a timer, then the jobs Streamline manages itself.
//
// Grouping costs the stable ordering — a job moves between sections when it
// starts or is paused — which is the trade this makes deliberately.
export function groupSchedules(items: Schedule[]): ScheduleGroup[] {
	const user = items.filter((s) => !s.system);
	const system = items.filter((s) => s.system);
	const running = user.filter((s) => s.running);
	const scheduled = user.filter((s) => !s.running);
	// Typed here, not inferred through the `.filter()` below: contextual typing
	// from the function's return type doesn't flow through a method call, so
	// each `key` would otherwise widen to `string` and fail against
	// `ScheduleGroupKey`.
	const groups: ScheduleGroup[] = [
		{
			key: "running",
			label: i18n.schedule_group_running(),
			rows: running,
			count: false,
		},
		{
			key: "scheduled",
			label: i18n.schedule_group_scheduled(),
			rows: scheduled,
			count: false,
		},
		{
			key: "system",
			label: i18n.settings_system(),
			rows: system,
			count: false,
		},
	];
	return groups.filter((g) => g.rows.length > 0);
}
