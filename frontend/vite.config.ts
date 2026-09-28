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
			adapter: adapter({ pages: '../internal/ui/build', assets: '../internal/ui/build' }),
			paths: { base: '/__gateway/ui' }
		})
	],
	server: {
		proxy: { '^/__gateway/(?!ui)': process.env.GATEWAY ?? 'http://127.0.0.1:8080' }
	}
});
