<script lang="ts">
	import { config } from '$lib/entities/config/store.svelte';
	import { status } from '$lib/entities/status/store.svelte';
	import { ago, mb } from '$lib/shared/lib/api';
	import Card from '$lib/shared/ui/Card.svelte';
	import Selector from '$lib/shared/ui/Selector.svelte';

	const s = $derived(status.value);
	const options = $derived(
		(s?.bundles ?? []).map((b) => ({
			value: b.version,
			label: b.version,
			desc: `${mb(b.zip_bytes)} · ${ago(b.mod_time)}`,
			badge: b.version === s?.default ? 'current' : undefined
		}))
	);
	const share = $derived.by(() => {
		const d = s?.decisions ?? {};
		const total = Object.values(d).reduce((a, b) => a + b, 0);
		return total ? Math.round(((d.default ?? 0) / total) * 100) : null;
	});
</script>

<Card title="Default bundle" id="default" class="lg:col-span-4">
	{#snippet actions()}
		{#if share !== null}<span class="badge badge-success">serving {share}%</span>{/if}
	{/snippet}
	<div class="mono text-[29px] leading-9 font-semibold">{s?.default ?? '…'}</div>
	<div class="field">
		<span class="field-label">Change default</span>
		<Selector
			title="Default bundle"
			{options}
			bind:value={config.draft.defaultBundle}
			placeholder="Newest bundle"
			mono
		/>
		<span class="field-desc"
			>Takes effect on Save. Visitors with a cookie keep their bundle until their next decision.</span
		>
	</div>
</Card>
