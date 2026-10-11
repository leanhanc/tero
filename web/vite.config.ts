/// <reference types="vitest/config" />
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

// The build lands inside the Go package that embeds it, so `go build` ships
// the dashboard in the binary.
export default defineConfig({
	plugins: [react()],
	build: {
		outDir: '../internal/dashboard/dist',
		// make web clears the old build itself, keeping the tracked .gitkeep
		// that lets Go embed the folder before anything is built.
		emptyOutDir: false,
		// The polyfill would be an inline script, which the Content-Security-Policy
		// forbids; every browser Tero supports has module preload built in.
		modulePreload: { polyfill: false },
	},
	test: {
		environment: 'happy-dom',
	},
});
