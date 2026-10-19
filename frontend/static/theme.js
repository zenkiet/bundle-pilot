// Runs before first paint, so a dark theme never flashes light. A file, not inline: the CSP
// allows only scripts it can name.
try {
	const t = localStorage.getItem('theme');
	if (t === 'dark' || (t !== 'light' && matchMedia('(prefers-color-scheme: dark)').matches))
		document.documentElement.classList.add('dark');
} catch {
	// storage blocked: the app sets the theme once it loads
}
