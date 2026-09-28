<script lang="ts">
	import { Cloud, Package } from '@lucide/svelte';
	import { status } from '$lib/entities/status/store.svelte';
	import Card from '$lib/shared/ui/Card.svelte';

	const s = $derived(status.value);
	const remote = $derived(!!s?.sync);
</script>

<Card title="Destination">
	<div class="flex items-center gap-2">
		{#if remote}<Cloud size={16} class="text-fg-2" />{:else}<Package
				size={16}
				class="text-fg-2"
			/>{/if}
		<span class="token mono">{s?.source ?? '…'}</span>
	</div>
	<dl class="meta">
		<dt>Follows</dt>
		<dd>the <span class="mono">source</span> block in config</dd>
		<dt>Visible after</dt>
		<dd>
			{remote
				? 'the upload, and on every gateway after its next sync'
				: 'the upload, this gateway only'}
		</dd>
		{#if s?.sync}<dt>Bucket</dt>
			<dd>{s.sync.error || `${s.sync.objects} zips listed`}</dd>{/if}
	</dl>
</Card>
