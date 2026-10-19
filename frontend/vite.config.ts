import tailwindcss from '@tailwindcss/vite';
import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [
		tailwindcss(),
		sveltekit({
			compilerOptions: {
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},
			adapter: adapter({ pages: '../internal/ui/build/app', assets: '../internal/ui/build/app' }),
			paths: { base: '/__gateway/ui' },
			// Prerendered pages carry it as a meta tag, with hashes of SvelteKit's own inline
			// scripts; the gateway adds frame-ancestors, which a meta tag cannot set.
			csp: {
				mode: 'hash',
				directives: {
					'default-src': ['self'],
					'script-src': ['self'],
					'style-src': ['self', 'unsafe-inline'],
					'img-src': ['self', 'data:'],
					'object-src': ['none'],
					'base-uri': ['self'],
					'form-action': ['self']
				}
			}
		})
	],
	server: {
		proxy: { '^/__gateway/(?!ui)': process.env.GATEWAY ?? 'http://127.0.0.1:8080' }
	}
});
