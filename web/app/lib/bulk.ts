// Bulk helpers shared by the movies and series selection bars.
//
// The REST API has no batch endpoints, so a bulk action is N single-item
// requests. Run them a few at a time (a 200-title library would otherwise open
// 200 sockets at once) and report a tally rather than failing the whole set on
// the first error — a partial success is still worth telling the user about.

export type BulkResult = {
	ok: number;
	failed: number;
	firstError?: string;
};

export async function runBulk<T>(
	items: T[],
	fn: (item: T) => Promise<unknown>,
	concurrency = 4,
): Promise<BulkResult> {
	const queue = [...items];
	const res: BulkResult = { ok: 0, failed: 0 };

	async function worker() {
		for (;;) {
			const item = queue.shift();
			if (item === undefined) return;
			try {
				await fn(item);
				res.ok++;
			} catch (e) {
				res.failed++;
				if (!res.firstError) res.firstError = (e as Error)?.message;
			}
		}
	}

	await Promise.all(
		Array.from({ length: Math.min(concurrency, items.length) }, worker),
	);
	return res;
}

export function plural(n: number, one: string, many = one + "s"): string {
	return `${n} ${n === 1 ? one : many}`;
}
