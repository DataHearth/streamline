import { createMutation, useQueryClient } from "@tanstack/svelte-query";
import { api, errorText } from "./api";
import { toast } from "./toast";
import type { Schedule, ScheduleList } from "./types";
import { m as i18n } from "./paraglide/messages.js";

export type ScheduleVerb = "pause" | "resume" | "run";

// Shared by the desktop row's three icon buttons and the touch action sheet.
// Both patch the same cache entry on success, so it lives here rather than
// being written twice with two chances to drift.
//
// Keyed by job name rather than captured at construction: the sheet's target
// changes every time it opens.
export function createScheduleActions() {
	const qc = useQueryClient();

	const SUCCESS_MESSAGE: Record<ScheduleVerb, string> = {
		pause: i18n.status_paused(),
		resume: i18n.schedule_resumed(),
		run: i18n.schedule_triggered(),
	};

	function verbMutation(verb: ScheduleVerb) {
		return createMutation<Schedule, Error, string>(() => ({
			mutationFn: (name) =>
				api<Schedule>(`/schedules/${encodeURIComponent(name)}/${verb}`, {
					method: "POST",
				}),
			onSuccess: (resp) => {
				qc.setQueryData(["schedules"], (prev: ScheduleList | undefined) => ({
					items: (prev?.items ?? []).map((s) =>
						s.name === resp.name ? resp : s,
					),
				}));
				toast.info(`${SUCCESS_MESSAGE[verb]} ${resp.name}`);
			},
			onError: (err) => toast.err(errorText(err)),
		}));
	}

	return {
		pause: verbMutation("pause"),
		resume: verbMutation("resume"),
		run: verbMutation("run"),
	};
}
