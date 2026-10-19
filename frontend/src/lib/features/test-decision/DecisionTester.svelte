<script lang="ts">
	import { decide, type Decision } from '#lib/entities/config/api.ts';
	import { config } from '#lib/entities/config/store.svelte.ts';
	import { message } from '#lib/shared/lib/api.ts';
	import Banner from '#lib/shared/ui/Banner.svelte';
	import Card from '#lib/shared/ui/Card.svelte';

	let facts = $state('{"backend": "07.30.2026", "storeID": 2020210}');
	let result = $state<Decision | null>(null);
	let error = $state('');
	const explain = $derived.by(() => {
		if (!result) return '';
		const [kind, id] = result.via.split(':');
		if (kind === 'rule') return `Rule “${id}” matched`;
		if (kind === 'backend') return `Backend table row ${id}`;
		if (kind === 'native') return 'The default needs a newer native app: the closest older bundle';
		return 'Default bundle, nothing matched';
	});

	async function run() {
		try {
			result = await decide(facts, config.draft);
			error = result.error ?? '';
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
			>On the draft, unsaved edits included. Sets no cookie.</span
		>
	</div>
	{#if error}
		<Banner status="error" title="Refused" desc={error} />
	{:else if result}
		<Banner status="success" title={result.bundle} desc={explain} />
	{/if}
	{#if result?.why.length}
		<ul class="flex flex-col gap-1">
			{#each result.why as line (line)}<li class="desc mono">{line}</li>{/each}
		</ul>
	{/if}
</Card>
