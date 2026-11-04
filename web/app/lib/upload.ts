import { ApiError, type ApiErrorBody } from "./api";

export type UploadEntry = { file: File; path: string };
export type UploadAccepted = { record_id: number; skipped_folders?: string[] };

const API = "/api/v1";

// A folder picked or dropped names itself in every path. Paths are sent
// relative to what it holds, so the top segment goes when every file shares it.
// An artist upload keeps it: the server matches folder names to albums, and that
// name is the album's when one album's folder is dropped.
function stripTop(entries: UploadEntry[]): UploadEntry[] {
	const first = entries[0]?.path.split("/")[0];
	if (first === undefined) return entries;
	const shared = entries.every((e) => {
		const parts = e.path.split("/");
		return parts.length > 1 && parts[0] === first;
	});
	if (!shared) return entries;
	return entries.map((e) => ({ file: e.file, path: e.path.slice(first.length + 1) }));
}

// webkitRelativePath is empty on a drop, so a dropped folder is walked through
// its FileSystemEntry. Every entry is taken before the first await: the items
// of a DataTransfer are gone once the drop handler yields.
export async function entriesFromDrop(dt: DataTransfer, keepTop = false): Promise<UploadEntry[]> {
	const roots: FileSystemEntry[] = [];
	for (const item of Array.from(dt.items)) {
		if (item.kind !== "file") continue;
		const entry = item.webkitGetAsEntry();
		if (entry) roots.push(entry);
	}
	const out: UploadEntry[] = [];
	await Promise.all(roots.map((r) => walk(r, "", out)));
	return keepTop ? out : stripTop(out);
}

async function walk(entry: FileSystemEntry, dir: string, out: UploadEntry[]) {
	const path = dir + entry.name;
	if (entry.isFile) {
		const file = await new Promise<File>((resolve, reject) =>
			(entry as FileSystemFileEntry).file(resolve, reject),
		);
		out.push({ file, path });
		return;
	}
	if (!entry.isDirectory) return;
	const reader = (entry as FileSystemDirectoryEntry).createReader();
	// readEntries hands back a batch at a time and an empty one at the end.
	for (;;) {
		const batch = await new Promise<FileSystemEntry[]>((resolve, reject) => reader.readEntries(resolve, reject));
		if (batch.length === 0) return;
		await Promise.all(batch.map((child) => walk(child, path + "/", out)));
	}
}

export function entriesFromInput(files: FileList, keepTop = false): UploadEntry[] {
	const entries = Array.from(files).map((file) => ({ file, path: file.webkitRelativePath || file.name }));
	return keepTop ? entries : stripTop(entries);
}

// The picked or dropped folder's own name, which stripTop removes from the
// paths; it becomes the upload's title.
export function folderName(src: DataTransfer | FileList): string | undefined {
	if (src instanceof FileList) {
		const rel = src[0]?.webkitRelativePath;
		return rel?.includes("/") ? rel.split("/")[0] : undefined;
	}
	for (const item of Array.from(src.items)) {
		const entry = item.kind === "file" ? item.webkitGetAsEntry() : null;
		if (entry?.isDirectory) return entry.name;
	}
	return undefined;
}

const DISC_DIR = /^(cd|disc|disk)\s*\d+$/i;

// An artist upload sends only the folders that match an album. The server
// decides again on what it receives; leaving the others out just saves the
// bytes. A disc subfolder belongs to the folder above it, as on the server.
export function withoutFolders(entries: UploadEntry[], skipped: string[]): UploadEntry[] {
	if (skipped.length === 0) return entries;
	const gone = new Set(skipped);
	return entries.filter((e) => {
		const parts = e.path.split("/");
		parts.pop();
		if (gone.has(parts.join("/") || ".")) return false;
		if (parts.length > 0 && DISC_DIR.test(parts.at(-1)?.trim() ?? "")) {
			parts.pop();
			return !gone.has(parts.join("/") || ".");
		}
		return true;
	});
}

// XMLHttpRequest, not fetch: fetch has no upload progress. Resolves with the
// 202 body and rejects with the same ApiError `api` throws, so errorText reads
// it. `url` is an API path.
export function uploadFiles(
	url: string,
	entries: UploadEntry[],
	onProgress: (sent: number, total: number) => void,
	signal?: AbortSignal,
): Promise<UploadAccepted> {
	return new Promise((resolve, reject) => {
		const xhr = new XMLHttpRequest();
		xhr.open("POST", url.startsWith(API) ? url : API + url);
		xhr.withCredentials = true;
		xhr.setRequestHeader("Accept", "application/json");
		xhr.upload.onprogress = (e) => {
			if (e.lengthComputable) onProgress(e.loaded, e.total);
		};
		xhr.onload = () => {
			if (xhr.status === 401) {
				location.href = `/login?next=${encodeURIComponent(location.pathname + location.search)}`;
				return;
			}
			const text = xhr.responseText;
			let parsed: unknown = null;
			try {
				parsed = text ? JSON.parse(text) : null;
			} catch {
				parsed = null;
			}
			if (xhr.status >= 200 && xhr.status < 300) {
				resolve(parsed as UploadAccepted);
				return;
			}
			const body = parsed && typeof parsed === "object" ? (parsed as ApiErrorBody) : null;
			const err = new ApiError(xhr.status, body?.message || text.trim().slice(0, 300) || xhr.statusText, body);
			err.bare = !text;
			reject(err);
		};
		xhr.onerror = () => reject(new TypeError("upload failed"));
		xhr.onabort = () => reject(new DOMException("aborted", "AbortError"));
		if (signal?.aborted) {
			reject(new DOMException("aborted", "AbortError"));
			return;
		}
		signal?.addEventListener("abort", () => xhr.abort(), { once: true });
		const form = new FormData();
		for (const e of entries) form.append("file", e.file, e.path);
		xhr.send(form);
	});
}
