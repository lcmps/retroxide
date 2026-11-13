import { render } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import LoadMoreSentinel from './LoadMoreSentinel.svelte';

type Callback = (entries: Array<Pick<IntersectionObserverEntry, 'isIntersecting'>>) => void;

/** Stands in for the browser's IntersectionObserver, letting a test report visibility. */
class FakeIntersectionObserver {
	static instances: FakeIntersectionObserver[] = [];
	observed: Element[] = [];
	disconnected = false;
	constructor(readonly callback: Callback) {
		FakeIntersectionObserver.instances.push(this);
	}
	observe(element: Element): void {
		this.observed.push(element);
	}
	disconnect(): void {
		this.disconnected = true;
	}
	unobserve(): void {}
	takeRecords(): IntersectionObserverEntry[] {
		return [];
	}
}

describe('LoadMoreSentinel', () => {
	beforeEach(() => {
		FakeIntersectionObserver.instances = [];
		vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver);
	});
	afterEach(() => vi.unstubAllGlobals());

	it('calls onvisible when the sentinel comes near the viewport, and not otherwise', () => {
		const onvisible = vi.fn();
		render(LoadMoreSentinel, { onvisible, key: 1 });
		const observer = FakeIntersectionObserver.instances.at(-1)!;
		expect(observer.observed).toHaveLength(1);

		observer.callback([{ isIntersecting: false }]);
		expect(onvisible).not.toHaveBeenCalled();
		observer.callback([{ isIntersecting: true }]);
		expect(onvisible).toHaveBeenCalledTimes(1);
	});

	it('re-observes when the key changes, so a short page keeps loading', async () => {
		const onvisible = vi.fn();
		const { rerender } = render(LoadMoreSentinel, { onvisible, key: 1 });
		const first = FakeIntersectionObserver.instances.at(-1)!;
		await rerender({ onvisible, key: 2 });
		const second = FakeIntersectionObserver.instances.at(-1)!;
		expect(first.disconnected).toBe(true);
		expect(second).not.toBe(first);
		expect(second.observed).toHaveLength(1);
	});
});
