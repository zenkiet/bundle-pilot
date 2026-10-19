<script lang="ts">
	import { untrack } from 'svelte';
	import { Copy, KeyRound, Trash2 } from '@lucide/svelte';
	import { status } from '#lib/entities/status/store.svelte.ts';
	import {
		createToken,
		listTokens,
		revokeToken,
		type Scope,
		type Token
	} from '#lib/entities/token/api.ts';
	import { ago, message } from '#lib/shared/lib/api.ts';
	import Banner from '#lib/shared/ui/Banner.svelte';
	import Card from '#lib/shared/ui/Card.svelte';
	import Dialog from '#lib/shared/ui/Dialog.svelte';
	import Selector, { type Option } from '#lib/shared/ui/Selector.svelte';
	import { toast } from '#lib/shared/ui/Toast.svelte';

	const scopes: Option<Scope>[] = [
		{ value: 'read', label: 'read', desc: 'Status, config and history' },
		{ value: 'upload', label: 'upload', desc: 'Also adds new bundles, for CI' },
		{ value: 'admin', label: 'admin', desc: 'Everything the admin can do' }
	];
	let tokens = $state<Token[]>([]);
	let name = $state('');
	let scope = $state<Scope>('upload');
	let secret = $state('');
	let revoking = $state<Token | null>(null);
	let confirm = $state(false);
	let busy = $state(false);
	const ready = $derived(!!status.value?.auth);

	const refresh = () =>
		listTokens().then(
			(t) => (tokens = t),
			(e) => toast(message(e), { error: true })
		);
	$effect(() => {
		if (ready) untrack(refresh);
	});

	async function act(run: () => Promise<void>) {
		busy = true;
		try {
			await run();
			await refresh();
		} catch (e) {
			toast(message(e), { error: true });
		} finally {
			busy = false;
		}
	}
	const create = () =>
		act(async () => {
			secret = await createToken(name.trim(), scope);
			name = '';
		});
	const revoke = (t: Token) =>
		act(async () => {
			await revokeToken(t.name);
			confirm = false;
			toast(`${t.name} revoked`);
		});
</script>

<Card title="API tokens" hint="Bearer tokens for CI and scripts" id="tokens" class="lg:col-span-4">
	{#if !ready}
		<p class="sup">Set an admin password first: until then the admin API is open to anyone.</p>
	{:else}
		{#if secret}
			<Banner status="success" title="Copy the token now" desc="The gateway shows it this once.">
				<div class="flex items-center gap-2">
					<code class="mono min-w-0 flex-1 text-xs break-all">{secret}</code>
					<button
						class="btn btn-sm"
						onclick={() => navigator.clipboard.writeText(secret).then(() => toast('Token copied'))}
						><Copy size={14} />Copy</button
					>
				</div>
			</Banner>
		{/if}
		<div class="flex flex-col">
			{#each tokens as t (t.name)}
				<div class="item py-1.5">
					<div class="flex min-w-0 flex-1 flex-col">
						<span class="mono truncate font-medium">{t.name}</span>
						{#if t.created}<span class="desc">created {ago(t.created)}</span>{/if}
					</div>
					<span class="badge">{t.scope}</span>
					<button
						class="btn btn-ghost btn-sm btn-icon"
						aria-label="Revoke {t.name}"
						onclick={() => ((revoking = t), (confirm = true))}><Trash2 size={14} /></button
					>
				</div>
			{:else}
				<p class="sup">No tokens yet.</p>
			{/each}
		</div>
		<div class="flex flex-wrap items-end gap-2">
			<label class="field min-w-32 flex-1"
				><span class="field-label">Name</span><input
					class="input mono"
					bind:value={name}
					placeholder="ci"
					autocomplete="off"
				/></label
			>
			<Selector title="Scope" options={scopes} bind:value={scope} />
			<button class="btn" onclick={create} disabled={busy || !name.trim()}
				><KeyRound size={14} />Create</button
			>
		</div>
	{/if}
</Card>

<Dialog
	bind:open={confirm}
	title="Revoke {revoking?.name}?"
	subtitle="Takes effect at once"
	width={400}
>
	<p>Scripts using it get 401 from now on. Make a new token to replace it.</p>
	{#snippet footer()}
		<button class="btn" onclick={() => (confirm = false)}>Cancel</button>
		<button class="btn btn-destructive" onclick={() => revoking && revoke(revoking)} disabled={busy}
			>Revoke</button
		>
	{/snippet}
</Dialog>
