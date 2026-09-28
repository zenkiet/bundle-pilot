<script lang="ts">
	import { resolve } from '$app/paths';
	import { Upload } from '@lucide/svelte';
	import { status } from '$lib/entities/status/store.svelte';
	import BundleMenu from '$lib/features/manage-bundle/BundleMenu.svelte';
	import { ago, mb } from '$lib/shared/lib/api';
	import Card from '$lib/shared/ui/Card.svelte';

	const s = $derived(status.value);
</script>

<Card title="Bundles" id="bundles" class="gap-1 lg:col-span-4">
	{#snippet actions()}
		<span class="badge">{s?.bundles.length ?? 0} on disk</span>
		<a class="btn btn-ghost btn-sm" href={resolve('/upload')}><Upload size={16} />Upload</a>
	{/snippet}
	<div class="flex flex-col">
		{#each s?.bundles ?? [] as b (b.version)}
			<div class="item py-1.5">
				<div class="flex min-w-0 flex-1 flex-col">
					<span class="mono truncate font-medium">{b.version}</span>
					<span class="desc">{mb(b.zip_bytes)} · {b.files} files · {ago(b.mod_time)}</span>
				</div>
				{#if b.version === s?.default}<span class="badge badge-green">default</span>{/if}
				<BundleMenu version={b.version} subtitle="{mb(b.zip_bytes)} · {ago(b.mod_time)}" />
			</div>
		{:else}
			<p class="sup">No bundles on disk.</p>
		{/each}
	</div>
</Card>
