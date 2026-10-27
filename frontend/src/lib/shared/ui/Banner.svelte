<script lang="ts">
	import type { Snippet } from 'svelte';
	import { CircleAlert, CircleCheck, Info, TriangleAlert } from '@lucide/svelte';

	let {
		status,
		title,
		desc,
		children
	}: {
		status: 'info' | 'warning' | 'error' | 'success';
		title: string;
		desc?: string;
		children?: Snippet;
	} = $props();
	const icons = { info: Info, warning: TriangleAlert, error: CircleAlert, success: CircleCheck };
	const Icon = $derived(icons[status]);
</script>

<div
	class="banner banner-{status}"
	role={status === 'info' || status === 'success' ? 'status' : 'alert'}
>
	<div class="banner-head">
		<Icon size={16} />
		<div class="flex min-w-0 flex-1 flex-col">
			<span class="banner-title">{title}</span>
			{#if desc}<span class="banner-desc">{desc}</span>{/if}
			{#if children}<div class="banner-desc mt-1">{@render children()}</div>{/if}
		</div>
	</div>
</div>
