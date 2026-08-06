import { getContext, setContext } from "svelte";
import { fetchAuthConfig } from "./auth_api";

// READONLY_HINT is the tooltip/title shown on locked mutation controls.
export const READONLY_HINT =
	"Read-only mode — configuration changes are disabled on this instance.";

// ConfigStore holds deploy-level config flags surfaced to the SPA. read_only
// comes from /auth/config (set when the instance runs with config.read_only);
// it gates every config-mutating control. It never toggles at runtime.
class ConfigStore {
	readOnly = $state(false);

	async hydrate() {
		try {
			this.readOnly = (await fetchAuthConfig()).read_only;
		} catch {
			// Leave readOnly=false on failure — the backend still rejects
			// mutations with ErrReadOnly, so this only affects the UI hint.
		}
	}
}

export const config = new ConfigStore();

const CONFIG_FORM = Symbol("config-form");

// markConfigForm flags the subtree as writing back to the YAML config, so the
// form primitives inside lock themselves on a read-only instance. A plain
// `<fieldset disabled>` can't express this: its descendants have no way to opt
// back in, and some controls inside these forms must stay live (see below).
export function markConfigForm() {
	setContext(CONFIG_FORM, true);
}

// readOnlyLock reports whether this control should refuse input. Call once at
// component init; the returned getter is reactive. Controls that only *read*
// remote state — Plex PIN sign-in, Plex section discovery — deliberately skip
// it: an operator whose config lives outside the app still needs them to
// obtain the token and section key they are going to write into that file.
export function readOnlyLock(): () => boolean {
	const inForm = getContext(CONFIG_FORM) === true;
	return () => inForm && config.readOnly;
}
