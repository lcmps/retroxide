import { getBootstrap } from '#lib/api/client.js';
import { ApiClientError } from '#lib/api/errors.js';
import type { Bootstrap, Profile } from '#lib/api/types.js';
import { setDefaultFont } from '#lib/appearance.js';

export type SetupState = 'required' | 'ready';

/**
 * The answer to the one request the app makes before its first paint: whether setup is
 * required, the default font, and the signed-in profile, which the shell reads instead of
 * asking for it again.
 */
export class BootstrapSession {
	profile = $state<Profile | null>(null);

	readonly #fetch: () => Promise<Bootstrap>;
	readonly #applyFont: (font: string | null | undefined) => void;

	constructor(fetch: () => Promise<Bootstrap> = getBootstrap, applyFont: (font: string | null | undefined) => void = setDefaultFont) {
		this.#fetch = fetch;
		this.#applyFont = applyFont;
	}

	/**
	 * Loads the bootstrap. A refused session is retried once anonymously (the client has
	 * cleared it by then); an unreachable backend is treated as configured so the normal error
	 * paths apply.
	 */
	async load(): Promise<SetupState> {
		let bootstrap: Bootstrap;
		try {
			bootstrap = await this.#fetchOnceMore();
		} catch {
			this.profile = null;
			return 'ready';
		}
		this.#applyFont(bootstrap.appearance.font);
		this.profile = bootstrap.profile;
		return bootstrap.setup_required ? 'required' : 'ready';
	}

	/**
	 * Returns the bootstrap's profile and forgets it. The shell mounts again after a sign-out and
	 * a new sign-in in the same tab; that account must fetch its own profile, not reuse this one.
	 */
	takeProfile(): Profile | null {
		const profile = this.profile;
		this.profile = null;
		return profile;
	}

	async #fetchOnceMore(): Promise<Bootstrap> {
		try {
			return await this.#fetch();
		} catch (error) {
			if (error instanceof ApiClientError && error.status === 401) return this.#fetch();
			throw error;
		}
	}
}

export const bootstrapSession = new BootstrapSession();
