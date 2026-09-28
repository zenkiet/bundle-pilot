export type Theme = 'light' | 'dark' | 'system';

function read(): Theme {
	try {
		const t = localStorage.getItem('theme');
		return t === 'light' || t === 'dark' ? t : 'system';
	} catch {
		return 'system';
	}
}

class ThemeStore {
	value = $state<Theme>(read());

	set(t: Theme) {
		this.value = t;
		try {
			if (t === 'system') localStorage.removeItem('theme');
			else localStorage.setItem('theme', t);
		} catch {
			/* private mode */
		}
		apply(t);
	}

	cycle() {
		this.set(this.value === 'light' ? 'dark' : this.value === 'dark' ? 'system' : 'light');
	}
}

function apply(t: Theme) {
	const dark =
		t === 'dark' || (t === 'system' && matchMedia('(prefers-color-scheme: dark)').matches);
	document.documentElement.classList.toggle('dark', dark);
}

export const theme = new ThemeStore();
matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => apply(theme.value));
