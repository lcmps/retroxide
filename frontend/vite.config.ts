import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig, loadEnv } from 'vite';

export default defineConfig(({ mode }) => {
	const env = loadEnv(mode, '.', '');
	const apiProxyTarget = env.VITE_API_PROXY_TARGET ?? 'http://127.0.0.1:3000';

	return {
		plugins: [
			sveltekit({
				compilerOptions: {
					// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
					runes: ({ filename }) =>
						filename.split(/[/\\]/).includes('node_modules') ? undefined : true
				},
				// Hash mode pins the inline bootstrap script in the static pages' CSP <meta>.
				// frame-ancestors cannot be set via <meta>; the Go server sends it as a header.
				csp: {
					mode: 'hash',
					directives: {
						'default-src': ['self'],
						'script-src': ['self'],
						// bits-ui/floating-ui position popovers with inline style attributes.
						'style-src': ['self', 'unsafe-inline'],
						// Cover art and downloads are fetched with the bearer token, then shown via blob: URLs.
						'img-src': ['self', 'blob:', 'data:'],
						'font-src': ['self'],
						'connect-src': ['self'],
						'object-src': ['none'],
						'base-uri': ['self'],
						'form-action': ['self']
					}
				},
				// Output lands in the Go package that embeds it into the single binary.
				adapter: adapter({
					pages: '../interface/web/dist',
					assets: '../interface/web/dist',
					fallback: 'index.html',
					precompress: true
				})
			})
		],
		server: {
			proxy: {
				'/api': {
					target: apiProxyTarget,
					changeOrigin: true
				},
				'/health': {
					target: apiProxyTarget,
					changeOrigin: true
				}
			}
		}
	};
});
