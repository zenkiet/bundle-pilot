<script lang="ts">
	let {
		label,
		value,
		pct,
		variant = ''
	}: {
		label: string;
		value?: string;
		pct: number;
		variant?: '' | 'success' | 'neutral';
	} = $props();
</script>

<div class="progress">
	<div class="flex items-baseline justify-between gap-2">
		<span class="mono truncate text-[13px] font-medium">{label}</span>
		{#if value}<span class="sup flex-none">{value}</span>{/if}
	</div>
	<div class="track">
		<div class="fill {variant ? `fill-${variant}` : ''}" style="width: {pct}%"></div>
	</div>
</div>
