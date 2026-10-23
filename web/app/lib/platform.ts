// Apple keyboards say ⌘ where every other platform says Ctrl. The shortcut
// handlers accept both; this only decides which one a hint shows.
// `userAgentData` is Chromium-only ("macOS"); Safari and Firefox still fill
// `navigator.platform` ("MacIntel", "iPhone"). An iPad reports "MacIntel",
// which is right here: its keyboards carry ⌘ too.
const platform =
	typeof navigator === "undefined"
		? ""
		: ((navigator as Navigator & { userAgentData?: { platform?: string } }).userAgentData?.platform ??
			navigator.platform);

export const isApple = /mac|iphone|ipad|ipod/i.test(platform);

// The modifier as a keyboard hint shows it: "⌘" or "Ctrl".
export const modKey = isApple ? "⌘" : "Ctrl";
