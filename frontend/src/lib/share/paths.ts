/** A share link inside the app: /share/ and a base64url token, nothing else. */
const SHARE_PATH = /^\/share\/[A-Za-z0-9_-]+$/;

/**
 * Where to go after signing in: back to a share link when that is where the person came from,
 * otherwise the library. Anything but a plain share path is ignored, so the login page can never
 * be used to send someone elsewhere.
 */
export function safeReturnPath(next: string | null): string {
	return next && SHARE_PATH.test(next) ? next : '/library';
}

/** The login page for someone signed out at path, remembering a share link to come back to. */
export function loginPathFor(path: string): string {
	return SHARE_PATH.test(path) ? `/login?${new URLSearchParams({ next: path })}` : '/login';
}

/** The share link's token, when path is a share link. */
export function shareToken(path: string): string | null {
	return SHARE_PATH.test(path) ? path.slice('/share/'.length) : null;
}
