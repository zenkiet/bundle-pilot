<script lang="ts" module>
	import type { Component } from 'svelte';

	export interface MenuItem {
		label: string;
		icon?: Component<{ size?: number | string }>;
		kbd?: string;
		destructive?: boolean;
		disabled?: boolean;
		divider?: boolean;
		run: () => void;
	}
</script>

<script lang="ts">
	import type { Snippet } from 'svelte';
	import { Ellipsis } from '@lucide/svelte';
	import { clickOutside } from '$lib/shared/lib/actions';
	import { media } from '$lib/shared/lib/media.svelte';
	import Sheet from './Sheet.svelte';

	let {
		items,
		title,
		subtitle,
		label = 'Actions',
		align = 'right',
		trigger
	}: {
		items: MenuItem[];
		title?: string;
		subtitle?: string;
		label?: string;
		align?: 'left' | 'right';
		trigger?: Snippet;
	} = $props();

	let open = $state(false);
	function pick(i: MenuItem) {
		open = false;
		i.run();
	}
</script>

{#snippet rows(lg: boolean)}
	{#each items as i (i.label)}
		{#if i.divider}<div class="menu-divider"></div>{/if}
		<button
			type="button"
			class="mi {lg ? 'px-2 py-3' : ''} {i.destructive ? 'mi-destructive' : ''}"
			disabled={i.disabled}
			onclick={() => pick(i)}
		>
			{#if i.icon}<i.icon size={16} />{/if}
			<span class="grow-1">{i.label}</span>
			{#if i.kbd}<span class="kbd">{i.kbd}</span>{/if}
		</button>
	{/each}
{/snippet}

<div class="relative flex-none" use:clickOutside={() => !media.compact && (open = false)}>
	<button
		type="button"
		class={trigger ? 'contents' : 'btn btn-ghost btn-sm btn-icon'}
		aria-label={label}
		aria-haspopup="menu"
		aria-expanded={open}
		onclick={() => (open = !open)}
	>
		{#if trigger}{@render trigger()}{:else}<Ellipsis size={16} />{/if}
	</button>
	{#if open && !media.compact}
		<div class="popover menu top-full mt-1 {align === 'right' ? 'right-0' : 'left-0'}" role="menu">
			{@render rows(false)}
		</div>
	{/if}
</div>
{#if open && media.compact}
	<Sheet {title} {subtitle} onclose={() => (open = false)}>
		<div class="flex flex-col" role="menu">{@render rows(true)}</div>
	</Sheet>
{/if}
