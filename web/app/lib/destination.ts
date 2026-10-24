// Where a saved media server, download client or indexer sends its requests,
// reduced the way the server compares it (internal/config/destination.go): a
// path, a trailing slash, letter case or a scheme's default port is not a move.
// Moving one is refused until its secret is entered again, so the edit forms
// compare against the saved destination to say so before the save does.

export function urlDestination(raw: string): string {
	const s = raw.trim();
	try {
		const u = new URL(s);
		if (u.host) return `${u.protocol}//${u.host}`;
	} catch {
		// Not an absolute URL: compared as written, like the server does.
	}
	return s.toLowerCase().replace(/\/+$/, "");
}

export function hostDestination(host: string, port: number | undefined, useSSL: boolean): string {
	return `${useSSL ? "https" : "http"}://${host.trim().toLowerCase()}:${port ?? ""}`;
}
