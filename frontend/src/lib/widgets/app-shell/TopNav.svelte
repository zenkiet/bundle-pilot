<script lang="ts">
	import { page } from '$app/state';
	import { Search } from '@lucide/svelte';
	import ReviewChanges from '$lib/features/save-config/ReviewChanges.svelte';

	let { onsearch }: { onsearch: () => void } = $props();
	const titles: Record<string, [string, string]> = {
		upload: ['Upload bundle', 'Zips only. Verified before they can serve.'],
		settings: ['Settings', 'What the setup wizard asked, saved together as one config.pb.']
	};
	const [title, sub] = $derived(
		titles[page.url.pathname.split('/').filter(Boolean).at(-1) ?? ''] ?? [
			'Overview',
			'Rules first, then the backend table, then the default bundle.'
		]
	);
</script>

<header class="flex min-h-12 items-center gap-4 border-b border-line px-4 py-2">
	<div class="flex min-w-0 items-baseline gap-3">
		<h1 class="text-xl leading-7 font-semibold">{title}</h1>
		<span class="sup hidden truncate xl:inline">{sub}</span>
	</div>
	<div class="ml-auto flex flex-none items-center gap-2">
		<button class="btn" onclick={onsearch}>
			<Search size={16} /><span class="hidden text-fg-2 lg:inline">Search or run a command</span
			><span class="kbd hidden lg:inline-flex">⌘K</span>
		</button>
		<ReviewChanges />
	</div>
</header>
