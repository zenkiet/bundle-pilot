<script lang="ts">
	import { create } from '@bufbuild/protobuf';
	import { Plus, X } from '@lucide/svelte';
	import { RuleSchema, type Rule } from '$lib/entities/config/gen/bundlepilot/config/v1/config_pb';
	import { fromRows, toRows, type WhenRow } from '$lib/entities/config/when';
	import Banner from '$lib/shared/ui/Banner.svelte';
	import Dialog from '$lib/shared/ui/Dialog.svelte';
	import Selector from '$lib/shared/ui/Selector.svelte';

	let {
		open = $bindable(false),
		rule,
		position,
		bundles,
		onsave,
		ondelete
	}: {
		open?: boolean;
		rule: Rule | null;
		position: number;
		bundles: string[];
		onsave: (r: Rule) => void;
		ondelete?: () => void;
	} = $props();

	let id = $state('');
	let note = $state('');
	let bundle = $state('');
	let until = $state('');
	let rows = $state<WhenRow[]>([]);
	const hint = '2020210, "10%", ">=2.0.0 <3.0.0", ["a","b"]';
	const options = $derived(bundles.map((b) => ({ value: b, label: b })));
	const preview = $derived(
		rows
			.filter((r) => r.fact.trim())
			.map((r) => `${r.fact.trim()} = ${r.value.trim() || '…'}`)
			.join(' and ')
	);

	$effect(() => {
		if (!open) return;
		id = rule?.id ?? '';
		note = rule?.note ?? '';
		bundle = rule?.bundle ?? bundles[0] ?? '';
		until = rule?.until ?? '';
		rows = rule ? toRows(rule.when) : [{ fact: '', value: '' }];
	});

	function submit() {
		onsave(
			create(RuleSchema, {
				id: id.trim(),
				note: note.trim(),
				bundle,
				until: until.trim(),
				when: fromRows(rows)
			})
		);
		open = false;
	}
</script>

<Dialog
	bind:open
	title={rule ? 'Edit rule' : 'New rule'}
	subtitle="Position {position}, checked before the backend table"
>
	<div class="grid gap-4 sm:grid-cols-2">
		<label class="field"
			><span class="field-label">Name</span><input
				class="input mono"
				bind:value={id}
				placeholder="pilot-store"
			/></label
		>
		<div class="field">
			<span class="field-label">Serve bundle</span><Selector
				title="Serve bundle"
				{options}
				bind:value={bundle}
				mono
			/>
		</div>
		<label class="field sm:col-span-2"
			><span class="field-label">Note <span class="text-xs font-normal">· optional</span></span
			><input class="input" bind:value={note} placeholder="Why this rule exists" /></label
		>
	</div>
	<div class="field gap-2">
		<span class="field-label"
			>Conditions <span class="text-xs font-normal">· all must match</span></span
		>
		{#each rows as row, i (i)}
			<div class="grid grid-cols-[1fr_1.4fr_32px] gap-2">
				<input class="input mono" bind:value={row.fact} placeholder="storeID" />
				<input class="input mono" bind:value={row.value} placeholder={hint} />
				<button
					class="btn btn-ghost btn-icon"
					onclick={() => (rows = rows.filter((_, j) => j !== i))}
					aria-label="Remove condition"><X size={16} /></button
				>
			</div>
		{/each}
		<button
			class="btn btn-ghost btn-sm self-start"
			onclick={() => (rows = [...rows, { fact: '', value: '' }])}
			><Plus size={16} />Add condition</button
		>
	</div>
	<label class="field"
		><span class="field-label"
			>Expires <span class="text-xs font-normal">· optional, YYYY-MM-DD or RFC 3339</span></span
		><input class="input mono sm:w-52" bind:value={until} placeholder="2026-10-31" /></label
	>
	<Banner
		status="info"
		title="Serves {bundle || '…'} when {preview || 'nothing is required'}"
		desc={until ? `Until ${until}` : 'No expiry'}
	/>
	{#snippet footer()}
		{#if ondelete}<button
				class="btn btn-destructive mr-auto"
				onclick={() => {
					ondelete?.();
					open = false;
				}}>Delete rule</button
			>{/if}
		<button class="btn" onclick={() => (open = false)}>Cancel</button>
		<button class="btn btn-primary" onclick={submit} disabled={!bundle}
			>{rule ? 'Save rule' : 'Add rule'}</button
		>
	{/snippet}
</Dialog>
