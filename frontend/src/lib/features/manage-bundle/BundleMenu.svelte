<script lang="ts">
	import { Ban, CircleCheck, ExternalLink, Link, Star, Trash2 } from '@lucide/svelte';
	import { deleteBundle, setBlocked } from '#lib/entities/bundle/api.ts';
	import { config } from '#lib/entities/config/store.svelte.ts';
	import { status } from '#lib/entities/status/store.svelte.ts';
	import { message, root } from '#lib/shared/lib/api.ts';
	import Dialog from '#lib/shared/ui/Dialog.svelte';
	import Menu from '#lib/shared/ui/Menu.svelte';
	import { toast } from '#lib/shared/ui/Toast.svelte';

	let { version, subtitle }: { version: string; subtitle?: string } = $props();
	let confirm = $state(false);
	let blocking = $state(false);
	let busy = $state(false);
	const isDefault = $derived(status.value?.default === version);
	const blocked = $derived(!!status.value?.bundles.find((b) => b.version === version)?.blocked);
	const url = () => location.origin + root(`/?bundle=${encodeURIComponent(version)}`);

	async function block(on: boolean) {
		busy = true;
		try {
			await setBlocked(version, on);
			toast(on ? `${version} blocked: nothing routes to it now` : `${version} serves again`);
			blocking = false;
			await status.refresh();
		} catch (e) {
			toast(message(e), { error: true });
		} finally {
			busy = false;
		}
	}

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
		blocked
			? { label: 'Unblock', icon: CircleCheck, divider: true, run: () => block(false) }
			: {
					label: 'Block now',
					icon: Ban,
					destructive: true,
					disabled: isDefault,
					divider: true,
					run: () => (blocking = true)
				},
		{
			label: 'Delete zip',
			icon: Trash2,
			destructive: true,
			disabled: isDefault,
			run: () => (confirm = true)
		}
	]}
/>

<Dialog bind:open={blocking} title="Block {version}?" subtitle="Takes effect at once" width={400}>
	<p>
		Rules and backend rows that point at it pause, visitors pinned to it move on with their next
		page, and over-the-air updaters can no longer install it. The zip stays, so Unblock brings it
		back.
	</p>
	{#snippet footer()}
		<button class="btn" onclick={() => (blocking = false)}>Cancel</button>
		<button class="btn btn-destructive" onclick={() => block(true)} disabled={busy}
			>Block now</button
		>
	{/snippet}
</Dialog>

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
