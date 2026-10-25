import { untrack } from "svelte";
import { activeRoute } from "@roxi/routify";

// A bottom sheet or a full-screen layer closes on the phone's Back, and never
// outlives the page it was opened on (#140).
//
// Back: Chromium's CloseWatcher hands the system Back button and gesture to
// the topmost open watcher, so nested layers close one at a time and nothing
// touches history. A layer faked into history instead had to slip past
// Routify's patched pushState, and every list page's replaceState of its own
// query rewrote the faked entry, so Back reverted the filters picked in the
// sheet. Where CloseWatcher is missing (Safari, Firefox) Back stays a page
// navigation, which the second rule turns into a close.
//
// Page change: a layer is closed as soon as the route's path moves off the one
// it was opened on. The query is left out on purpose: a filter sheet rewrites
// it on every tap and must stay open. Layers mounted in AppShell (the add
// flow) are the ones this matters for; a route's own sheets unmount with it.
//
// Escape stays with each layer's own handler. Chromium also routes Escape to
// the topmost watcher, which would act a second time on top of that handler
// (and close a sheet whose Escape only collapses it), so a watcher spent by
// Escape does nothing.
//
// Call it while the component initialises. `isOpen` is read reactively; a
// sheet that is only mounted while open passes `() => true`. `close` may leave
// the layer open (a sheet that collapses first): a watcher fires once, so it
// is armed again for the next Back.
export function closeOnBack(isOpen: () => boolean, close: () => void) {
	let round = $state(0);
	const rearm = () => {
		if (isOpen()) round++;
	};
	$effect(() => {
		if (!isOpen()) return;
		void round;
		return untrack(() =>
			arm(() => {
				if (!isOpen()) return;
				close();
				rearm();
			}, rearm),
		);
	});
}

type Watcher = { onclose: (() => void) | null; destroy(): void };
type WatcherCtor = new () => Watcher;

function arm(onBack: () => void, rearm: () => void): () => void {
	let done = false;
	const fire = () => {
		if (done) return;
		done = true;
		onBack();
	};

	const Ctor = (globalThis as { CloseWatcher?: WatcherCtor }).CloseWatcher;
	let watcher: Watcher | null = null;
	try {
		watcher = Ctor ? new Ctor() : null;
	} catch {
		// Refused: the layer still closes on Escape, its own controls and a
		// page change.
	}

	// Set while an Escape keydown is dispatched; the watcher's close follows
	// that dispatch in the same task.
	let escape = false;
	const onKey = (e: KeyboardEvent) => {
		if (e.key !== "Escape") return;
		escape = true;
		setTimeout(() => (escape = false));
	};
	if (watcher) {
		window.addEventListener("keydown", onKey, true);
		watcher.onclose = () => {
			if (!escape) return fire();
			// The layer's own handler has the Escape. The watcher is spent, so
			// arm another for a later Back if the layer is still open.
			done = true;
			rearm();
		};
	}

	let path: string | null = null;
	const off = activeRoute.subscribe((r) => {
		if (!r) return;
		const p = (r.url ?? "").split("?")[0] || "/";
		if (path === null) path = p;
		else if (p !== path) fire();
	});

	return () => {
		done = true;
		watcher?.destroy();
		window.removeEventListener("keydown", onKey, true);
		off();
	};
}
