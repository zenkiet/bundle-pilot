<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { LogOut, RefreshCw } from '@lucide/svelte';
	import { reloadGateway } from '$lib/entities/bundle/api';
	import { config } from '$lib/entities/config/store.svelte';
	import { status } from '$lib/entities/status/store.svelte';
	import { ago, message } from '$lib/shared/lib/api';
	import { session } from '$lib/shared/lib/session.svelte';
	import Card from '$lib/shared/ui/Card.svelte';
	import { toast } from '$lib/shared/ui/Toast.svelte';

	const s = $derived(status.value);
	let busy = $state(false);

	async function reload() {
		busy = true;
		try {
			toast(`Reloaded in ${(await reloadGateway()).reload_ms} ms`);
			await status.refresh();
		} catch (e) {
			toast(message(e), { error: true });
		} finally {
			busy = false;
		}
	}
</script>

<Card title="Gateway" hint="not in config.pb" class="lg:col-span-4">
	<dl class="meta">
		<dt>Version</dt>
		<dd class="mono truncate" title={s?.version}>{s?.version ?? '…'}</dd>
		<dt>Signing</dt>
		<dd class="flex items-center gap-2">
			<span class="badge {s?.signing ? 'badge-green' : ''}">{s?.signing ? 'on' : 'off'}</span><span
				class="sup">BUNDLE_PUBKEY env</span
			>
		</dd>
		<dt>Last reload</dt>
		<dd>{s ? ago(s.loaded_at) : '…'}<span class="sup"> · {s?.reloads ?? 0} total</span></dd>
		<dt>Session</dt>
		<dd class="mono">
			{config.saved.auth?.username ?? 'no password set'}<span class="sup font-sans">
				· this tab</span
			>
		</dd>
	</dl>
	<div class="flex flex-wrap gap-2">
		<button class="btn btn-sm" onclick={reload} disabled={busy}
			><RefreshCw size={14} />Reload now</button
		>
		{#if session.header}<button
				class="btn btn-ghost btn-sm"
				onclick={() => {
					session.clear();
					goto(resolve('/login'));
				}}><LogOut size={14} />Sign out</button
			>{/if}
	</div>
</Card>
