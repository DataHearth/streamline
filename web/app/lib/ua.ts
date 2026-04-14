export type ParsedAgent = {
	browser: string;
	os: string;
};

export function parseUA(ua: string | undefined | null): ParsedAgent {
	if (!ua) return { browser: "Browser", os: "Desktop" };
	const s = ua.toLowerCase();
	return { browser: detectBrowser(s), os: detectOS(s) };
}

function detectBrowser(s: string): string {
	if (s.includes("edg/")) return "Edge";
	if (s.includes("opr/") || s.includes("opera")) return "Opera";
	if (s.includes("chrome/") && !s.includes("chromium")) return "Chrome";
	if (s.includes("chromium")) return "Chromium";
	if (s.includes("firefox/")) return "Firefox";
	if (s.includes("safari/")) return "Safari";
	return "Browser";
}

function detectOS(s: string): string {
	if (s.includes("windows nt")) return "Windows";
	if (s.includes("mac os x") || s.includes("macintosh")) return "macOS";
	if (s.includes("linux")) return "Linux";
	if (s.includes("cros")) return "ChromeOS";
	return "Desktop";
}
