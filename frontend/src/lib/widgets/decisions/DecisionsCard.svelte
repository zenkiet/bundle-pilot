<script lang="ts">
	import { status } from '$lib/entities/status/store.svelte';
	import Card from '$lib/shared/ui/Card.svelte';
	import ProgressBar from '$lib/shared/ui/ProgressBar.svelte';

	const rows = $derived(
		Object.entries(status.value?.decisions ?? {}).sort(([, a], [, b]) => b - a)
	);
	const total = $derived(rows.reduce((n, [, v]) => n + v, 0));
	const variant = (via: string) =>
		via === 'default' ? 'neutral' : via.startsWith('rule:') ? 'success' : '';
</script>

<Card title="Decisions" hint="since start" class="lg:col-span-4">
	{#if rows.length === 0}
		<p class="sup">No <span class="mono">/data</span> calls yet.</p>
	{:else}
		<div class="flex flex-col gap-3">
			{#each rows as [via, n] (via)}
				<ProgressBar
					label={via}
					value="{n} · {Math.round((n / total) * 100)}%"
					pct={(n / total) * 100}
					variant={variant(via)}
				/>
			{/each}
		</div>
	{/if}
	<span class="sup mt-auto">Counted from gateway decisions, not per visitor.</span>
</Card>
