<script lang="ts">
	import { testDecision, type Decision } from '$lib/entities/status/api';
	import { config } from '$lib/entities/config/store.svelte';
	import { toRows } from '$lib/entities/config/when';
	import { message } from '$lib/shared/lib/api';
	import Banner from '$lib/shared/ui/Banner.svelte';
	import Card from '$lib/shared/ui/Card.svelte';

	let facts = $state('{"backend": "07.30.2026", "storeID": 2020210}');
	let result = $state<Decision | null>(null);
	let error = $state('');
	const explain = $derived.by(() => {
		if (!result) return '';
		const [kind, id] = result.via.split(':');
		if (kind === 'rule') {
			const r = config.saved.rules.find((x) => x.id === id);
			return r
				? `Rule “${id}” matched: ${toRows(r.when)
						.map((w) => `${w.fact} = ${w.value}`)
						.join(', ')}`
				: `Rule “${id}” matched`;
		}
		return kind === 'backend' ? `Backend table row ${id}` : 'Default bundle, nothing matched';
	});

	async function run() {
		try {
			result = await testDecision(facts);
			error = '';
		} catch (e) {
			result = null;
			error = message(e);
		}
	}
</script>

<Card title="Decision tester" id="tester" class="lg:col-span-4">
	<label class="field"
		><span class="field-label"
			>Facts <span class="text-xs font-normal">· JSON, as the app would post them</span></span
		><textarea class="input mono min-h-16 resize-y py-1.5" bind:value={facts} spellcheck="false"
		></textarea></label
	>
	<div class="flex items-center gap-2">
		<button class="btn btn-primary btn-sm" onclick={run}>Decide</button><span class="sup"
			>Dry run, no cookie is set.</span
		>
	</div>
	{#if result}
		<Banner status="success" title={result.bundle} desc={explain} />
	{:else if error}
		<Banner status="error" title="Refused" desc={error} />
	{/if}
</Card>
