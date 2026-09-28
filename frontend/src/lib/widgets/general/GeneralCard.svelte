<script lang="ts">
	import { config } from '$lib/entities/config/store.svelte';
	import Card from '$lib/shared/ui/Card.svelte';
	import Selector from '$lib/shared/ui/Selector.svelte';

	const dates = [
		{ value: 'MM.dd.YYYY', label: 'MM.dd.YYYY', desc: 'default · 07.31.2026' },
		{ value: 'dd.MM.YYYY', label: 'dd.MM.YYYY', desc: '31.07.2026' },
		{ value: 'YYYY-MM-DD', label: 'YYYY-MM-DD', desc: '2026-07-31' },
		{ value: 'M.d.YYYY', label: 'M.d.YYYY', desc: '7.31.2026' }
	];
</script>

<Card
	title="General"
	hint="browser tab, sidebar heading, backend keys"
	id="general"
	class="lg:col-span-8"
>
	<div class="grid gap-3 md:grid-cols-2">
		<label class="field"
			><span class="field-label">Project name</span><input
				class="input"
				bind:value={config.draft.projectName}
				placeholder="POS web"
			/></label
		>
		<label class="field"
			><span class="field-label"
				>Environment <span class="font-normal text-fg-2">· optional</span></span
			><input class="input" bind:value={config.draft.environment} placeholder="production" /></label
		>
	</div>
	<div class="field md:w-90">
		<span class="field-label">Date format for backend keys</span><Selector
			title="Date format"
			options={dates}
			bind:value={config.draft.dateFormat}
			placeholder="MM.dd.YYYY"
			mono
		/><span class="field-desc">Backend table keys and the backend fact must use this format.</span>
	</div>
</Card>
