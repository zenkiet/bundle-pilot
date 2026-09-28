<script lang="ts">
	import { Menu, Search } from '@lucide/svelte';
	import { fade, fly } from 'svelte/transition';
	import { X } from '@lucide/svelte';
	import SideNav from './SideNav.svelte';

	import { status } from '$lib/entities/status/store.svelte';

	let { onsearch }: { onsearch: () => void } = $props();
	const title = $derived(status.value?.project_name || 'Bundle Pilot');
	let open = $state(false);
</script>

<header class="flex min-h-12 items-center gap-1 border-b border-line p-2">
	<button class="btn btn-ghost btn-icon" onclick={() => (open = true)} aria-label="Open navigation"
		><Menu size={16} /></button
	>
	<span class="large grow-1">{title}</span>
	<button class="btn btn-ghost btn-icon" onclick={onsearch} aria-label="Search or run a command"
		><Search size={16} /></button
	>
</header>

{#if open}
	<div
		class="scrim"
		transition:fade={{ duration: 120 }}
		onclick={() => (open = false)}
		role="presentation"
	></div>
	<div
		class="fixed inset-y-0 left-0 z-50 flex w-80 max-w-[85vw] flex-col border-r border-line bg-surface shadow-high"
		transition:fly={{ x: -320, duration: 250 }}
		role="dialog"
		aria-modal="true"
		aria-label="Navigation"
	>
		<button
			class="btn btn-ghost btn-icon absolute top-2 right-2"
			onclick={() => (open = false)}
			aria-label="Close navigation"><X size={16} /></button
		>
		<SideNav onnavigate={() => (open = false)} />
	</div>
{/if}
