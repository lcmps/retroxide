/**
 * Async operation controller for managing loading, error, and status states.
 * Reduces boilerplate when handling async operations in components.
 */
export function createAsyncController() {
	let loading = $state(false);
	let error = $state<Error | null>(null);
	let status = $state('');

	async function run<T>(
		fn: () => Promise<T>,
		options?: {
			onError?: (error: Error) => string;
		}
	): Promise<T> {
		loading = true;
		error = null;
		status = '';

		try {
			return await fn();
		} catch (caughtError) {
			const normalizedError =
				caughtError instanceof Error ? caughtError : new Error(String(caughtError));
			error = normalizedError;
			status = options?.onError?.(normalizedError) ?? normalizedError.message;
			throw normalizedError;
		} finally {
			loading = false;
		}
	}

	return {
		get loading() {
			return loading;
		},
		get error() {
			return error;
		},
		get status() {
			return status;
		},
		reset() {
			error = null;
			status = '';
		},
		run
	};
}

export function resetAsyncError(controller: ReturnType<typeof createAsyncController>): void {
	controller.reset();
}