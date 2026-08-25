// Read a picked .torrent into the base64 the POST /torrents body wants.
// Shared by the desktop modal and the touch sheet so the two can't drift.

export type TorrentFileRead = { name: string; base64: string };

export async function readTorrentFile(file: File): Promise<TorrentFileRead> {
	if (!file.name.toLowerCase().endsWith(".torrent")) {
		throw new Error("Choose a .torrent file.");
	}
	const bytes = new Uint8Array(await file.arrayBuffer());
	let binary = "";
	for (const b of bytes) binary += String.fromCharCode(b);
	return { name: file.name, base64: btoa(binary) };
}

export const isMagnet = (value: string) =>
	value.trim().toLowerCase().startsWith("magnet:?");
