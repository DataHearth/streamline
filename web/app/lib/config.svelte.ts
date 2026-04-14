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
