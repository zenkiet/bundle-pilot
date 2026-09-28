<script lang="ts">
	import type { Snippet } from 'svelte';
	import { CircleAlert, CircleCheck, Info, TriangleAlert } from '@lucide/svelte';

	let {
		status,
		title,
		desc,
		actions,
		children,
		class: cls = ''
	}: {
		status: 'info' | 'warning' | 'error' | 'success';
		title: string;
		desc?: string;
		actions?: Snippet;
		children?: Snippet;
		class?: string;
	} = $props();
	const icons = { info: Info, warning: TriangleAlert, error: CircleAlert, success: CircleCheck };
	const Icon = $derived(icons[status]);
</script>

<div
	class="banner banner-{status} {cls}"
	role={status === 'info' || status === 'success' ? 'status' : 'alert'}
>
	<div class="banner-head">
		<Icon size={16} />
		<div class="flex min-w-0 flex-1 flex-col">
			<span class="banner-title">{title}</span>
			{#if desc}<span class="banner-desc">{desc}</span>{/if}
			{#if children}<div class="banner-desc mt-1">{@render children()}</div>{/if}
		</div>
		{#if actions}<div class="-my-1 ml-auto flex flex-none items-center gap-2">
				{@render actions()}
			</div>{/if}
	</div>
</div>
