<script lang="ts">
	import { status } from '$lib/entities/status/store.svelte';
	import { ago, mb } from '$lib/shared/lib/api';
	import Card from '$lib/shared/ui/Card.svelte';

	const s = $derived(status.value);
</script>

<Card title="Health" class="lg:col-span-4">
	{#if s}
		<dl class="meta">
			<dt>Status</dt>
			<dd class="flex items-center gap-2">
				<span class="dot {s.last_error ? 'dot-error' : 'dot-success'}"></span>{s.last_error
					? s.last_error
					: `Serving · ${s.reloads} reloads`}
			</dd>
			<dt>Memory</dt>
			<dd>{mb(s.resident_bytes)} in RAM · {s.bundles.length} bundles</dd>
			<dt>Source</dt>
			<dd><span class="token mono">{s.source}</span></dd>
			{#if s.sync}
				<dt>Sync</dt>
				<dd class="flex items-center gap-2">
					<span class="dot {s.sync.error ? 'dot-error' : 'dot-success'}"></span><span
						class="truncate"
						>{s.sync.error || `${s.sync.objects} zips listed · ${ago(s.sync.at)}`}</span
					>
				</dd>
			{/if}
			<dt>Last reload</dt>
			<dd>{ago(s.loaded_at)} <span class="sup">· default from {s.default_from}</span></dd>
			<dt>Signing</dt>
			<dd class="flex items-center gap-2">
				Ed25519 <span class="badge {s.signing ? 'badge-green' : ''}"
					>{s.signing ? 'verified' : 'off'}</span
				>
			</dd>
		</dl>
	{:else}
		<p class="sup">{status.error || 'Loading…'}</p>
	{/if}
</Card>
