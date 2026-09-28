<script lang="ts">
	import { create } from '@bufbuild/protobuf';
	import { SourceSchema } from '$lib/entities/config/gen/bundlepilot/config/v1/config_pb';
	import { config } from '$lib/entities/config/store.svelte';
	import { status } from '$lib/entities/status/store.svelte';
	import { ago } from '$lib/shared/lib/api';
	import Card from '$lib/shared/ui/Card.svelte';

	const type = $derived(config.draft.source?.type === 's3' ? 's3' : 'local');
	const sync = $derived(status.value?.sync);
	const setType = (t: string) =>
		(config.draft.source =
			t === 's3' ? create(SourceSchema, { type: 's3', region: 'auto', poll: '30s' }) : undefined);
	const fields = [
		['bucket', 'Bucket', 'bundles'],
		['prefix', 'Prefix', 'prod/'],
		['endpoint', 'Endpoint · empty for AWS', 'https://<account>.r2.cloudflarestorage.com'],
		['region', 'Region', 'auto'],
		['poll', 'Poll every', '30s'],
		['accessKeyId', 'Access key id · empty = IAM chain', '']
	] as const;
</script>

<Card title="Bundle source" id="source" class="gap-2 lg:col-span-4">
	<div class="seg seg-fill" role="radiogroup" aria-label="Bundle source">
		<button
			class="seg-item {type === 'local' ? 'seg-on' : ''}"
			role="radio"
			aria-checked={type === 'local'}
			onclick={() => setType('local')}>Local</button
		>
		<button
			class="seg-item {type === 's3' ? 'seg-on' : ''}"
			role="radio"
			aria-checked={type === 's3'}
			onclick={() => setType('s3')}>S3 / R2</button
		>
	</div>
	{#if config.draft.source && type === 's3'}
		<div class="grid grid-cols-2 gap-2">
			{#each fields as [key, label, ph] (key)}
				<label class="field {key === 'endpoint' ? 'col-span-2' : ''}"
					><span class="field-label">{label}</span><input
						class="input mono"
						bind:value={config.draft.source[key]}
						placeholder={ph}
						autocomplete="off"
					/></label
				>
			{/each}
			<label class="field"
				><span class="field-label">Secret access key</span><input
					class="input mono"
					type="password"
					bind:value={config.draft.source.secretAccessKey}
					placeholder={config.saved.source?.secretAccessKey === '***' ? 'kept as stored' : ''}
					autocomplete="new-password"
				/></label
			>
		</div>
		{#if sync}
			<div class="sup flex items-center gap-2">
				<span class="dot {sync.error ? 'dot-error' : 'dot-success'}"></span><span class="truncate"
					>{sync.error ||
						`Connection OK · ${sync.objects} zips listed · synced ${ago(sync.at)}`}</span
				>
			</div>
		{/if}
	{:else}
		<span class="sup"
			>Zips are read from <span class="mono">versions/</span> next to the config. Nothing else to configure.</span
		>
	{/if}
</Card>
