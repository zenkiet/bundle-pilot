<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		title,
		hint,
		id,
		actions,
		children,
		class: cls = ''
	}: {
		title?: string;
		hint?: string;
		id?: string;
		actions?: Snippet;
		children: Snippet;
		class?: string;
	} = $props();
</script>

<section class="card scroll-mt-4 outline-none {cls}" {id} tabindex="-1">
	{#if title}
		<div class="flex items-center justify-between gap-2">
			<span class="large truncate"
				>{title}{#if hint}<span class="sup font-normal"> · {hint}</span>{/if}</span
			>
			{#if actions}<span class="flex flex-none items-center gap-1">{@render actions()}</span>{/if}
		</div>
	{/if}
	{@render children()}
</section>
