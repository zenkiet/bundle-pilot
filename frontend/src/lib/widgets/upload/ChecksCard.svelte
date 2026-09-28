<script lang="ts">
	import { CircleCheck } from '@lucide/svelte';
	import { status } from '$lib/entities/status/store.svelte';
	import Card from '$lib/shared/ui/Card.svelte';

	const checks = $derived([
		['Is a .zip', 'Anything else is skipped in the browser'],
		[
			'Named by version',
			'4.81.0.zip or a date in the configured format · same name replaces the zip'
		],
		['index.html at the root', 'Or under the folder named by <base href>'],
		status.value?.signing
			? ['Signed manifest', 'bundle.sha256 + bundle.sha256.sig match BUNDLE_PUBKEY']
			: ['Signing off', 'Set BUNDLE_PUBKEY to require bundle.sha256.sig'],
		['Under 256 MB', 'Larger zips are refused with 413']
	]);
</script>

<Card title="Checks before serving" class="gap-1">
	{#snippet actions()}<span class="badge">{checks.length}</span>{/snippet}
	<div class="flex flex-col">
		{#each checks as [title, desc] (title)}
			<div class="item px-2 py-1.5">
				<CircleCheck size={16} class="flex-none text-success" />
				<div class="flex min-w-0 flex-col">
					<span>{title}</span><span class="desc">{desc}</span>
				</div>
			</div>
		{/each}
	</div>
</Card>
