import { toast } from "./toast";
import type { PlexPinBegin, PlexPinPoll } from "./types";

const POLL_MS = 1500;
const TIMEOUT_MS = 5 * 60 * 1000;

type StartPlexPinOptions = {
	// onClientId fires as soon as the flow starts (before sign-in completes),
	// so callers can surface the client id immediately.
	onClientId?: (clientID: string) => void;
	onToken: (token: string, clientID: string) => void;
	// onDone fires on any terminal outcome (success or failure), so callers can
	// reset their busy state.
	onDone?: () => void;
};

// startPlexPin drives the Plex PIN sign-in: POST to begin, open the auth popup,
// then poll until the token arrives, the PIN expires, or it times out. Toasts
// cover progress and failures; success is signalled to the caller via onToken.
export async function startPlexPin({
	onClientId,
	onToken,
	onDone,
}: StartPlexPinOptions) {
	toast.info("Starting Plex sign-in…");
	let pinID = 0;
	let authURL = "";
	let clientID = "";
	try {
		const res = await fetch("/settings/media-servers/plex/pin", {
			method: "POST",
			credentials: "same-origin",
		});
		if (!res.ok) throw new Error(await res.text());
		const body = (await res.json()) as PlexPinBegin;
		pinID = body.pin_id;
		authURL = body.auth_url;
		clientID = body.client_id;
		onClientId?.(clientID);
	} catch (err) {
		toast.err(`Couldn't start Plex sign-in: ${errMsg(err)}`);
		onDone?.();
		return;
	}

	const popup = window.open(authURL, "plex-auth", "width=600,height=700");
	if (!popup) {
		toast.err(
			"Popup blocked — allow pop-ups for this site and click Connect again.",
		);
		onDone?.();
		return;
	}
	toast.info("Waiting for Plex sign-in — finish in the popup window.");

	const startedAt = Date.now();
	const tick = async () => {
		if (Date.now() - startedAt > TIMEOUT_MS) {
			toast.err("Plex sign-in timed out — click Connect to retry.");
			closePopup(popup);
			onDone?.();
			return;
		}
		try {
			const res = await fetch(`/settings/media-servers/plex/pin/${pinID}`, {
				credentials: "same-origin",
			});
			if (res.ok) {
				const body = (await res.json()) as PlexPinPoll;
				if (body.expired) {
					toast.err("Plex PIN expired — click Connect to retry.");
					closePopup(popup);
					onDone?.();
					return;
				}
				if (body.auth_token) {
					closePopup(popup);
					onToken(body.auth_token, clientID);
					onDone?.();
					return;
				}
			}
			// Otherwise the PIN is still pending, or the poll hit a transient
			// failure (plex.tv hiccup, rate-limit → our 502). Keep polling; only a
			// token, expiry, or the overall timeout ends the flow.
		} catch {
			// Transient network failure — keep polling until the timeout.
		}
		setTimeout(tick, POLL_MS);
	};
	setTimeout(tick, POLL_MS);
}

function errMsg(err: unknown) {
	return err instanceof Error ? err.message : String(err);
}

function closePopup(popup: Window) {
	try {
		popup.close();
	} catch {
		/* ignore */
	}
}
