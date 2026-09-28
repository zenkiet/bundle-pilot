<script lang="ts">
	import { ExternalLink, Link, Star, Trash2 } from '@lucide/svelte';
	import { deleteBundle } from '$lib/entities/bundle/api';
	import { config } from '$lib/entities/config/store.svelte';
	import { status } from '$lib/entities/status/store.svelte';
	import { message } from '$lib/shared/lib/api';
	import Dialog from '$lib/shared/ui/Dialog.svelte';
	import Menu from '$lib/shared/ui/Menu.svelte';
	import { toast } from '$lib/shared/ui/Toast.svelte';

	let { version, subtitle }: { version: string; subtitle?: string } = $props();
	let confirm = $state(false);
	let busy = $state(false);
	const isDefault = $derived(status.value?.default === version);
	const url = () => `${location.origin}/?bundle=${encodeURIComponent(version)}`;

	async function remove() {
		busy = true;
		try {
			await deleteBundle(version);
			toast(`${version} deleted`);
			confirm = false;
			await status.refresh();
		} catch (e) {
			toast(message(e), { error: true });
		} finally {
			busy = false;
		}
	}
</script>

<Menu
	title={version}
	{subtitle}
	items={[
		{
			label: 'Set as default',
			icon: Star,
			disabled: config.draft.defaultBundle === version,
			run: () => (config.draft.defaultBundle = version)
		},
		{
			label: 'Preview pinned',
			icon: ExternalLink,
			run: () => window.open(url(), '_blank', 'noopener')
		},
		{
			label: 'Copy link',
			icon: Link,
			run: () => navigator.clipboard.writeText(url()).then(() => toast('Link copied'))
		},
		{
			label: 'Delete zip',
			icon: Trash2,
			destructive: true,
			disabled: isDefault,
			divider: true,
			run: () => (confirm = true)
		}
	]}
/>

<Dialog
	bind:open={confirm}
	title="Delete {version}?"
	subtitle="Removed from disk and from the bucket"
	width={400}
>
	<p>
		Rules that point at it turn inactive. Visitors pinned to it fall through to the backend table on
		their next decision.
	</p>
	{#snippet footer()}
		<button class="btn" onclick={() => (confirm = false)}>Cancel</button>
		<button class="btn btn-destructive" onclick={remove} disabled={busy}>Delete zip</button>
	{/snippet}
</Dialog>
