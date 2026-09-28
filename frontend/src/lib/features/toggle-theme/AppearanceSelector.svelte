<script lang="ts">
	import Selector from '$lib/shared/ui/Selector.svelte';
	import { theme, type Theme } from './theme.svelte';

	let { size = 'md' }: { size?: 'sm' | 'md' | 'lg' } = $props();
	const options: { value: Theme; label: string; desc: string }[] = [
		{ value: 'light', label: 'Light', desc: 'Always light' },
		{ value: 'dark', label: 'Dark', desc: 'Always dark' },
		{ value: 'system', label: 'System', desc: 'Follows the device' }
	];
</script>

<Selector title="Appearance" {options} {size} value={theme.value} onchange={(v) => theme.set(v)} />
