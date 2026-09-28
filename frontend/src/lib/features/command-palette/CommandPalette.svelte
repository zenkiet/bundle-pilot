<script lang="ts" module>
	import type { Component } from 'svelte';

	export interface Command {
		id: string;
		group: string;
		label: string;
		hint?: string;
		icon?: Component<{ size?: number | string }>;
		run: () => void;
	}
</script>

<script lang="ts">
	import { Search } from '@lucide/svelte';
	import { fade, scale } from 'svelte/transition';

	let { commands, open = $bindable(false) }: { commands: Command[]; open?: boolean } = $props();
	let query = $state('');
	let cursor = $state(0);
	let input = $state<HTMLInputElement | null>(null);
	const matches = $derived(
		commands.filter((c) => c.label.toLowerCase().includes(query.toLowerCase()))
	);
	const groups = $derived([...new Set(matches.map((c) => c.group))]);

	export function show() {
		open = true;
		query = '';
		cursor = 0;
		queueMicrotask(() => input?.focus());
	}

	function key(e: KeyboardEvent) {
		if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
			e.preventDefault();
			if (open) open = false;
			else show();
			return;
		}
		if (!open) return;
		if (e.key === 'Escape') open = false;
		if (e.key === 'ArrowDown') cursor = Math.min(cursor + 1, matches.length - 1);
		if (e.key === 'ArrowUp') cursor = Math.max(cursor - 1, 0);
		if (e.key === 'Enter' && matches[cursor]) pick(matches[cursor]);
	}

	function pick(c: Command) {
		open = false;
		c.run();
	}
</script>

<svelte:window onkeydown={key} />

{#if open}
	<div
		class="scrim"
		transition:fade={{ duration: 100 }}
		onclick={() => (open = false)}
		role="presentation"
	></div>
	<div
		class="dialog top-3 right-3 left-3 max-h-120 md:top-24 md:right-auto md:left-1/2 md:w-160 md:-translate-x-1/2"
		transition:scale={{ duration: 120, start: 0.98 }}
		role="dialog"
		aria-label="Commands"
	>
		<div class="cp-input">
			<Search size={16} class="text-fg-2" />
			<input
				bind:this={input}
				class="min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-fg-2"
				placeholder="Search rules, bundles and actions…"
				bind:value={query}
				oninput={() => (cursor = 0)}
			/>
			<button class="btn btn-ghost btn-sm md:hidden" onclick={() => (open = false)}>Cancel</button>
			<span class="kbd hidden md:inline-flex">esc</span>
		</div>
		<div class="cp-list">
			{#each groups as g (g)}
				<div class="cp-group">{g}</div>
				{#each matches.filter((c) => c.group === g) as c (c.id)}
					<button
						class="cp-item {matches.indexOf(c) === cursor ? 'cp-hl' : ''}"
						onmouseenter={() => (cursor = matches.indexOf(c))}
						onclick={() => pick(c)}
					>
						{#if c.icon}<c.icon size={16} />{/if}
						<span class="grow-1">{c.label}</span>
						{#if c.hint}<span class="kbd">{c.hint}</span>{/if}
					</button>
				{/each}
			{:else}
				<div class="flex justify-center px-4 py-8 text-xs text-fg-2">No results for “{query}”</div>
			{/each}
		</div>
		<div class="cp-footer hidden md:flex">
			<span class="inline-flex items-center gap-1"
				><span class="kbd">↑</span><span class="kbd">↓</span> navigate</span
			>
			<span class="inline-flex items-center gap-1"><span class="kbd">↵</span> select</span>
			<span class="inline-flex items-center gap-1"><span class="kbd">esc</span> close</span>
		</div>
	</div>
{/if}
