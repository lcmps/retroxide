/**
 * Where the header's search and platform controls lead: the library with those filters. On the
 * library each keystroke replaces the history entry and keeps the order; from any other page the
 * first change adds one entry, so Back returns to that page.
 */
export function librarySearchTarget(current: { pathname: string; searchParams: Pick<URLSearchParams, 'toString'> }, platform: string, query: string): { href: string; replace: boolean } {
	const onLibrary = current.pathname === '/library';
	// On the library its other settings (the order, an open BIOS panel) stay; another page's
	// query string means nothing to the library.
	const params = new URLSearchParams(onLibrary ? current.searchParams.toString() : '');
	if (platform) params.set('platform', platform);
	else params.delete('platform');
	params.delete('q');
	if (query.trim()) params.set('q', query.trim());
	return { href: params.size > 0 ? `/library?${params}` : '/library', replace: onLibrary };
}
