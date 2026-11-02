import adapter from '@sveltejs/adapter-auto';
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
				// adapter-auto requires selecting a deployment adapter for production hosting.
				adapter: adapter()
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
