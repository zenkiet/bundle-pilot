<script lang="ts">
	import type { Snippet } from 'svelte';
	import { fade, scale } from 'svelte/transition';
	import { X } from '@lucide/svelte';
	import { media } from '$lib/shared/lib/media.svelte';
	import Sheet from './Sheet.svelte';

	let {
		open = $bindable(false),
		title,
		subtitle,
		width = 560,
		children,
		footer
	}: {
		open?: boolean;
		title: string;
		subtitle?: string;
		width?: number;
		children: Snippet;
		footer?: Snippet;
	} = $props();
	const close = () => (open = false);
</script>

<svelte:window
	onkeydown={open && !media.compact ? (e) => e.key === 'Escape' && close() : undefined}
/>

{#if open && media.compact}
	<Sheet {title} {subtitle} tall onclose={close} {footer}>
		<div class="flex flex-col gap-4 px-2 pt-2">{@render children()}</div>
	</Sheet>
{:else if open}
	<div class="scrim" transition:fade={{ duration: 120 }} onclick={close} role="presentation"></div>
	<div
		class="dialog top-1/2 left-1/2 w-[calc(100%-32px)] -translate-x-1/2 -translate-y-1/2"
		style="max-width: {width}px"
		transition:scale={{ duration: 150, start: 0.97 }}
		role="dialog"
		aria-modal="true"
		aria-label={title}
	>
		<div class="dialog-header">
			<div class="flex min-w-0 flex-col">
				<h2 class="text-xl leading-7 font-semibold">{title}</h2>
				{#if subtitle}<span class="sup">{subtitle}</span>{/if}
			</div>
			<button class="btn btn-ghost btn-icon" onclick={close} aria-label="Close"
				><X size={16} /></button
			>
		</div>
		<div class="dialog-content">{@render children()}</div>
		{#if footer}<div class="dialog-footer">{@render footer()}</div>{/if}
	</div>
{/if}
