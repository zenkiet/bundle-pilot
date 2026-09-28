<script lang="ts">
	import { Upload } from '@lucide/svelte';
	import FileDropzone from '$lib/features/upload-bundle/FileDropzone.svelte';
	import { queue } from '$lib/features/upload-bundle/queue.svelte';
	import UploadQueue from '$lib/features/upload-bundle/UploadQueue.svelte';
	import Card from '$lib/shared/ui/Card.svelte';
	import Switch from '$lib/shared/ui/Switch.svelte';
	import ChecksCard from '$lib/widgets/upload/ChecksCard.svelte';
	import DestinationCard from '$lib/widgets/upload/DestinationCard.svelte';

	const rejected = $derived(queue.skipped.map((i) => i.file.name).join(', '));
	const label = $derived(
		queue.running
			? 'Uploading…'
			: queue.queued
				? `Upload ${queue.queued} file${queue.queued === 1 ? '' : 's'}`
				: 'Nothing to upload'
	);
</script>

<div class="flex flex-col gap-3 p-3 md:gap-4 md:p-4">
	<div class="flex flex-col gap-2 md:flex-row md:items-center">
		<span class="large md:hidden">Upload bundle</span>
		<span class="sup md:mr-auto"
			>{queue.verified} verified · {queue.queued} to upload{#if queue.skipped.length}
				· {queue.skipped.length} skipped{/if}</span
		>
		<div class="flex gap-2 md:contents">
			<button
				class="btn flex-1 md:flex-none"
				onclick={() => queue.clearDone()}
				disabled={!queue.items.some((i) => i.status === 'verified' || i.status === 'skipped')}
				>Clear finished</button
			>
			<button
				class="btn btn-primary flex-1 md:flex-none"
				onclick={() => queue.start()}
				disabled={!queue.queued || queue.running}><Upload size={16} />{label}</button
			>
		</div>
	</div>
	<div class="grid grid-cols-1 gap-3 md:gap-4 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
		<div class="flex flex-col gap-3 md:gap-4">
			<Card>
				<div class="field">
					<span class="field-label"
						>Bundle zips <span class="text-xs font-normal">· .zip only, up to 256 MB each</span
						></span
					>
					<FileDropzone onfiles={(f) => queue.add(f)} error={rejected} />
					{#if rejected}<span class="field-status"
							>{rejected} skipped: only .zip files are accepted</span
						>{:else}<span class="field-desc"
							>Checked in the browser first, then uploaded and verified by the gateway.</span
						>{/if}
				</div>
			</Card>
			{#if queue.items.length}
				<UploadQueue />
			{:else}
				<div class="empty rounded-container border border-dashed border-line-2">
					<span class="large">Nothing queued</span>
					<span class="sup max-w-90"
						>Drop zips above. They are verified before they can serve, and the gateway reloads after
						each one.</span
					>
				</div>
			{/if}
		</div>
		<div class="flex flex-col gap-3 md:gap-4">
			<DestinationCard />
			<Card title="After upload" class="gap-1">
				<Switch
					bind:checked={queue.setDefault}
					label="Set the newest verified bundle as default"
					desc="Saves the config right after the last upload."
				/>
			</Card>
			<ChecksCard />
		</div>
	</div>
</div>
