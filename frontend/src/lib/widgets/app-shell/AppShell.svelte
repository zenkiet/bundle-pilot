<script lang="ts">
	import type { Snippet } from 'svelte';
	import MobileBar from './MobileBar.svelte';
	import MobileToolbar from './MobileToolbar.svelte';
	import SideNav from './SideNav.svelte';
	import TopNav from './TopNav.svelte';

	let { onsearch, children }: { onsearch: () => void; children: Snippet } = $props();
</script>

<div class="flex h-dvh">
	<aside class="hidden flex-none border-r border-line lg:block"><SideNav /></aside>
	<aside class="hidden flex-none border-r border-line md:block lg:hidden"><SideNav rail /></aside>
	<div class="flex min-w-0 flex-1 flex-col">
		<div class="hidden md:block"><TopNav {onsearch} /></div>
		<div class="md:hidden"><MobileBar {onsearch} /></div>
		<main class="min-h-0 flex-1 overflow-auto">{@render children()}</main>
		<div class="md:hidden"><MobileToolbar /></div>
	</div>
</div>
