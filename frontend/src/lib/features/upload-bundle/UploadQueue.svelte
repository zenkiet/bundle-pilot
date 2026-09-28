<script lang="ts">
	import { File, FileArchive, X } from '@lucide/svelte';
	import BundleMenu from '$lib/features/manage-bundle/BundleMenu.svelte';
	import { mb } from '$lib/shared/lib/api';
	import Card from '$lib/shared/ui/Card.svelte';
	import { queue, type ItemStatus } from './queue.svelte';

	const badge: Record<ItemStatus, [string, string]> = {
		verified: ['badge-success', 'verified'],
		uploading: ['badge-info', 'uploading'],
		queued: ['', 'queued'],
		error: ['badge-error', 'error'],
		skipped: ['badge-error', 'skipped']
	};
	const color = (s: ItemStatus) =>
		s === 'error' || s === 'skipped'
			? 'text-error'
			: s === 'verified'
				? 'text-success'
				: 'text-fg-2';
</script>

<Card
	title="Queue"
	hint="{queue.items.length} file{queue.items.length === 1 ? '' : 's'} · uploads run one at a time"
	class="gap-1"
>
	<div class="flex flex-col">
		{#each queue.items as it (it.id)}
			{@const [cls, text] = badge[it.status]}
			<div class="qrow {it.status === 'skipped' ? 'qrow-muted' : ''}">
				<span class="text-fg-2"
					>{#if it.status === 'skipped'}<File size={16} />{:else}<FileArchive
							size={16}
						/>{/if}</span
				>
				<div class="flex min-w-0 flex-col gap-1">
					<div class="flex items-center gap-2">
						<span class="mono truncate font-medium">{it.file.name}</span>
						<span class="sup hidden sm:inline">{mb(it.file.size)}</span>
						<span class="badge {cls}"
							>{text}{#if it.status === 'uploading'}
								{it.pct}%{/if}</span
						>
					</div>
					{#if it.status === 'uploading'}
						<div class="progress">
							<div class="track"><div class="fill" style="width: {it.pct}%"></div></div>
						</div>
					{/if}
					<span class="text-xs leading-4 {color(it.status)}">{it.note}</span>
				</div>
				<div class="flex items-center gap-1">
					{#if it.status === 'error'}<button
							class="btn btn-ghost btn-sm"
							onclick={() => queue.retry(it.id)}>Retry</button
						>{/if}
					{#if it.status === 'verified'}
						<BundleMenu
							version={it.file.name.replace(/\.zip$/i, '')}
							subtitle="Uploaded · {mb(it.file.size)}"
						/>
					{:else}
						<button
							class="btn btn-ghost btn-sm btn-icon"
							onclick={() => queue.remove(it.id)}
							aria-label="Remove"
							disabled={it.status === 'uploading'}><X size={16} /></button
						>
					{/if}
				</div>
			</div>
		{/each}
	</div>
</Card>
