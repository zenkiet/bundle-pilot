<script lang="ts">
	import { Upload } from '@lucide/svelte';

	let { onfiles, error = '' }: { onfiles: (files: FileList) => void; error?: string } = $props();
	let input = $state<HTMLInputElement | null>(null);
	let over = $state(false);

	function drop(e: DragEvent) {
		e.preventDefault();
		over = false;
		if (e.dataTransfer?.files.length) onfiles(e.dataTransfer.files);
	}
</script>

<button
	type="button"
	class="dropzone {over ? 'dropzone-active' : error ? 'dropzone-error' : ''}"
	onclick={() => input?.click()}
	ondragover={(e) => {
		e.preventDefault();
		over = true;
	}}
	ondragleave={() => (over = false)}
	ondrop={drop}
>
	<Upload size={20} class="text-fg-2" />
	<span>{over ? 'Release to add these zips' : 'Drop .zip bundles here, or choose files'}</span>
	<span class="sup"
		>One zip per bundle, named by its version: <span class="mono">4.81.0.zip</span> or
		<span class="mono">08.05.2026.zip</span> · up to 256 MB</span
	>
	<span class="btn btn-sm mt-1">Choose files</span>
</button>
<input
	bind:this={input}
	type="file"
	accept=".zip,application/zip"
	multiple
	class="hidden"
	onchange={(e) => {
		if (e.currentTarget.files?.length) onfiles(e.currentTarget.files);
		e.currentTarget.value = '';
	}}
/>
