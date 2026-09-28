<script lang="ts">
	import { Plus, X } from '@lucide/svelte';
	import { config } from '$lib/entities/config/store.svelte';
	import { status } from '$lib/entities/status/store.svelte';
	import Card from '$lib/shared/ui/Card.svelte';
	import Selector from '$lib/shared/ui/Selector.svelte';

	const bundles = $derived(status.value?.bundles.map((b) => b.version) ?? []);
	const options = $derived(bundles.map((b) => ({ value: b, label: b })));
	const rows = $derived(
		Object.entries(config.draft.backend).sort(([a], [b]) => a.localeCompare(b))
	);
	let newKey = $state('');
	const set = (map: Record<string, string>) => (config.draft.backend = map);

	function rename(from: string, to: string) {
		const { [from]: bundle, ...rest } = config.draft.backend;
		set(to.trim() ? { ...rest, [to.trim()]: bundle } : rest);
	}
	function add() {
		if (!newKey.trim()) return;
		set({ ...config.draft.backend, [newKey.trim()]: bundles[0] ?? '' });
		newKey = '';
	}
</script>

<Card title="Backend table" id="backend" class="lg:col-span-4">
	{#snippet actions()}
		<button class="btn btn-ghost btn-sm" onclick={add} disabled={!newKey.trim()}
			><Plus size={16} />Add row</button
		>
	{/snippet}
	<div class="grid grid-cols-[1fr_1fr_28px] items-center gap-2">
		<span class="field-label">Backend from</span><span class="field-label">Serves</span><span
		></span>
		{#each rows as [key, bundle] (key)}
			<input
				class="input input-sm mono"
				value={key}
				onchange={(e) => rename(key, e.currentTarget.value)}
			/>
			<Selector
				title="Bundle for {key}"
				size="sm"
				mono
				options={bundles.includes(bundle)
					? options
					: [...options, { value: bundle, label: `${bundle} (not on disk)` }]}
				value={bundle}
				onchange={(v) => set({ ...config.draft.backend, [key]: v })}
			/>
			<button
				class="btn btn-ghost btn-sm btn-icon"
				onclick={() => rename(key, '')}
				aria-label="Remove row"><X size={16} /></button
			>
		{/each}
		<input
			class="input input-sm mono"
			bind:value={newKey}
			placeholder="08.15.2026"
			onkeydown={(e) => e.key === 'Enter' && add()}
		/>
		<span class="sup col-span-2">Nearest key at or below the visitor’s backend wins.</span>
	</div>
</Card>
