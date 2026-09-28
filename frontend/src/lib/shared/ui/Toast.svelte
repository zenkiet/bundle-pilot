<script lang="ts" module>
	interface Toast {
		text: string;
		error?: boolean;
		action?: { label: string; run: () => void };
	}
	let current = $state<Toast | null>(null);
	let timer: ReturnType<typeof setTimeout> | undefined;

	export function toast(text: string, opts: Omit<Toast, 'text'> = {}) {
		current = { text, ...opts };
		clearTimeout(timer);
		timer = setTimeout(() => (current = null), opts.action ? 6000 : 3000);
	}
</script>

<script lang="ts">
	import { CircleAlert, CircleCheck, X } from '@lucide/svelte';
	import { fly } from 'svelte/transition';
</script>

{#if current}
	<div
		class="toast fixed right-3 bottom-20 left-3 z-50 w-auto md:right-4 md:bottom-4 md:left-auto md:w-100 {current.error
			? 'bg-error-muted text-error'
			: ''}"
		role="status"
		transition:fly={{ y: 8, duration: 150 }}
	>
		{#if current.error}<CircleAlert size={16} class="mt-0.5 flex-none" />{:else}<CircleCheck
				size={16}
				class="mt-0.5 flex-none"
			/>{/if}
		<span class="min-w-0 flex-1">{current.text}</span>
		<span class="-mr-1 flex flex-none items-center gap-2">
			{#if current.action}
				<button
					class="btn btn-sm"
					onclick={() => {
						current?.action?.run();
						current = null;
					}}>{current.action.label}</button
				>
			{/if}
			<button
				class="btn btn-ghost btn-sm btn-icon"
				style="background: transparent; color: inherit"
				onclick={() => (current = null)}
				aria-label="Dismiss"><X size={14} /></button
			>
		</span>
	</div>
{/if}
