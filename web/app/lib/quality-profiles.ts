import type { DefaultMedia, QualityProfile } from "@lib/types";
import { m as i18n } from "@lib/paraglide/messages.js";

export function defaultProfileName(
	profiles: QualityProfile[] | undefined,
	media: DefaultMedia,
) {
	return profiles?.find((p) => p.default_for?.includes(media))?.name ?? "";
}

export function serverDefaultLabel(
	profiles: QualityProfile[] | undefined,
	media: DefaultMedia,
) {
	const name = defaultProfileName(profiles, media);
	return name
		? i18n.quality_server_default_named({ name })
		: i18n.quality_server_default();
}
