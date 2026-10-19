<script lang="ts">
	import { onMount } from 'svelte';
	import { clone } from '@bufbuild/protobuf';
	import { RotateCcw } from '@lucide/svelte';
	import {
		AuthSchema,
		ConfigSchema,
		type Config
	} from '#lib/entities/config/gen/bundlepilot/config/v1/config_pb.ts';
	import { listHistory, loadRevision, type Revision } from '#lib/entities/config/history.ts';
	import { config } from '#lib/entities/config/store.svelte.ts';
	import { diff } from '#lib/features/save-config/diff.ts';
	import { saveAll } from '#lib/features/save-config/save.ts';
	import { ago, message } from '#lib/shared/lib/api.ts';
	import Card from '#lib/shared/ui/Card.svelte';
	import Dialog from '#lib/shared/ui/Dialog.svelte';
	import { toast } from '#lib/shared/ui/Toast.svelte';

	let revs = $state<Revision[]>([]);
	let picked = $state<Revision | null>(null);
	let next = $state<Config | null>(null);
	let confirm = $state(false);
	const changes = $derived(next ? diff(config.saved, next) : []);
	const kinds = { added: 'badge-green', changed: '', removed: 'badge-red' };

	const refresh = () =>
		listHistory().then(
			(r) => (revs = r),
			(e) => toast(message(e), { error: true })
		);
	onMount(refresh);

	async function pick(r: Revision) {
		picked = r;
		next = null;
		try {
			// A revert keeps the live admin account: it never brings back an old password.
			const c = await loadRevision(r.id);
			c.auth = config.saved.auth && clone(AuthSchema, config.saved.auth);
			next = c;
		} catch (e) {
			toast(message(e), { error: true });
		}
	}

	async function restore() {
		if (!next) return;
		config.reset();
		config.draft = clone(ConfigSchema, next);
		await saveAll();
		confirm = false;
		await refresh();
	}
</script>

<Card title="Saved configs" hint="newest first" class="gap-1 lg:col-span-5">
	<div class="flex flex-col">
		{#each revs as r (r.id)}
			<button
				class="item py-1.5 text-left {picked?.id === r.id ? 'bg-hover' : ''}"
				onclick={() => pick(r)}
			>
				<div class="flex min-w-0 flex-1 flex-col">
					<span class="font-medium">{new Date(r.at).toLocaleString()}</span>
					<span class="desc">{ago(r.at)} · by {r.by || 'unknown'}</span>
				</div>
			</button>
		{:else}
			<p class="sup">Nothing saved since history began.</p>
		{/each}
	</div>
</Card>

<Card title={picked ? 'Restoring it changes' : 'Pick a saved config'} class="gap-1 lg:col-span-7">
	{#snippet actions()}
		<button class="btn btn-sm" disabled={!changes.length} onclick={() => (confirm = true)}
			><RotateCcw size={14} />Restore</button
		>
	{/snippet}
	{#if picked && next}
		{#each changes as c (c.what + c.diff)}
			<div class="item">
				<div class="flex min-w-0 flex-1 flex-col">
					<span class="font-medium">{c.what}</span><span class="desc mono">{c.diff}</span>
				</div>
				<span class="badge {kinds[c.kind]}">{c.kind}</span>
			</div>
		{:else}
			<p class="sup">Same as the live config.</p>
		{/each}
		<span class="sup mt-2"
			>Secrets, tokens and blocked bundles are not part of history: a restore keeps the live ones.</span
		>
	{:else}
		<p class="sup">Choose an entry to see what restoring it would change.</p>
	{/if}
</Card>

<Dialog
	bind:open={confirm}
	title="Restore this config?"
	subtitle="{changes.length} change{changes.length === 1 ? '' : 's'}, saved and live at once"
	width={420}
>
	<p>
		{config.dirty ? 'Your unsaved edits are dropped. ' : ''}The gateway reloads with it, and the
		restore lands in history like any save.
	</p>
	{#snippet footer()}
		<button class="btn" onclick={() => (confirm = false)}>Cancel</button>
		<button class="btn btn-primary" onclick={restore} disabled={config.busy}>Save and reload</button
		>
	{/snippet}
</Dialog>
