/**
 * Returns a function that runs async tasks with at most `max` in flight; extra tasks wait in
 * FIFO order. A finishing task hands its slot directly to the next waiter, so the limit holds
 * even when new tasks arrive while one is being handed off.
 */
export function createLimiter(max: number) {
	let active = 0;
	const waiting: Array<() => void> = [];

	return async function run<T>(task: () => Promise<T>): Promise<T> {
		if (active >= max) {
			await new Promise<void>((resolve) => waiting.push(resolve));
		} else {
			active += 1;
		}
		try {
			return await task();
		} finally {
			const next = waiting.shift();
			if (next) {
				next();
			} else {
				active -= 1;
			}
		}
	};
}
