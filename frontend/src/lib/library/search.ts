/**
 * Where the header's search and platform controls lead: the library with those filters. On the
 * library each keystroke replaces the history entry and keeps the order; from any other page the
 * first change adds one entry, so Back returns to that page.
 */
export function librarySearchTarget(current: { pathname: string; searchParams: Pick<URLSearchParams, 'get'> }, platform: string, query: string): { href: string; replace: boolean } {
	const onLibrary = current.pathname === '/library';
	const params = new URLSearchParams();
	if (onLibrary) {
		for (const key of ['sort', 'order']) {
			const value = current.searchParams.get(key);
			if (value) params.set(key, value);
		}
	}
	if (platform) params.set('platform', platform);
	if (query.trim()) params.set('q', query.trim());
	return { href: params.size > 0 ? `/library?${params}` : '/library', replace: onLibrary };
}
