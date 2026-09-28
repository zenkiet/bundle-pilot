<script lang="ts">
	import type { Snippet } from 'svelte';
	import { fade, fly } from 'svelte/transition';
	import { X } from '@lucide/svelte';

	let {
		title,
		subtitle,
		tall = false,
		onclose,
		children,
		footer
	}: {
		title?: string;
		subtitle?: string;
		tall?: boolean;
		onclose: () => void;
		children: Snippet;
		footer?: Snippet;
	} = $props();
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && onclose()} />
<div class="scrim" transition:fade={{ duration: 120 }} onclick={onclose} role="presentation"></div>
<div
	class="sheet {tall ? 'h-[92dvh]' : ''}"
	transition:fly={{ y: 200, duration: 250 }}
	role="dialog"
	aria-modal="true"
	aria-label={title}
>
	<div class="handle"></div>
	{#if title}
		<div class="flex items-start justify-between gap-2 py-1 pr-2 pl-4">
			<div class="flex min-w-0 flex-col">
				<span class="text-xl leading-7 font-semibold">{title}</span>
				{#if subtitle}<span class="sup">{subtitle}</span>{/if}
			</div>
			<button class="btn btn-ghost btn-icon" onclick={onclose} aria-label="Close"
				><X size={16} /></button
			>
		</div>
	{/if}
	<div class="min-h-0 flex-1 overflow-auto px-2 pb-[max(env(safe-area-inset-bottom),20px)]">
		{@render children()}
	</div>
	{#if footer}
		<div class="dialog-footer pb-[max(env(safe-area-inset-bottom),16px)]">{@render footer()}</div>
	{/if}
</div>
