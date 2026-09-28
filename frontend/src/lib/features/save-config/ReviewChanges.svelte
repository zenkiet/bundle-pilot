<script lang="ts">
	import { config } from '$lib/entities/config/store.svelte';
	import { clickOutside } from '$lib/shared/lib/actions';
	import { media } from '$lib/shared/lib/media.svelte';
	import Sheet from '$lib/shared/ui/Sheet.svelte';
	import { diff } from './diff';
	import { saveAll } from './save';

	let { size = 'md' }: { size?: 'sm' | 'md' } = $props();
	let open = $state(false);
	const changes = $derived(diff(config.saved, config.draft));
	const summary = $derived(
		config.errors.length
			? `${config.errors.length} error${config.errors.length > 1 ? 's' : ''}`
			: `${config.issues.length} warning${config.issues.length === 1 ? '' : 's'}`
	);
	const kinds = { added: 'badge-green', changed: '', removed: 'badge-red' };

	async function save() {
		await saveAll();
		open = false;
	}
	function discard() {
		config.reset();
		open = false;
	}
</script>

{#snippet body()}
	<div class="flex flex-col p-1">
		{#each changes as c (c.what + c.diff)}
			<div class="item">
				<div class="flex min-w-0 flex-1 flex-col">
					<span class="font-medium">{c.what}</span><span class="desc mono">{c.diff}</span>
				</div>
				<span class="badge {kinds[c.kind]}">{c.kind}</span>
			</div>
		{/each}
		{#each config.errors as e (e)}
			<div class="item text-error"><span class="text-xs">{e}</span></div>
		{/each}
		{#each config.issues as i (i)}
			<div class="item text-fg-2"><span class="text-xs">{i}</span></div>
		{/each}
	</div>
{/snippet}

{#snippet buttons()}
	<button class="btn" onclick={discard}>Discard all</button>
	<button
		class="btn btn-primary flex-1 md:flex-none"
		onclick={save}
		disabled={config.busy || config.errors.length > 0}
	>
		Save and reload<span class="kbd badge-tint hidden border-transparent md:inline-flex">⌘S</span>
	</button>
{/snippet}

<div class="relative" use:clickOutside={() => !media.compact && (open = false)}>
	<button
		class="btn btn-primary btn-{size}"
		onclick={() => (open = !open)}
		disabled={!config.dirty}
	>
		Review{#if changes.length}<span class="badge badge-tint">{changes.length}</span>{/if}
	</button>
	{#if open && !media.compact}
		<div class="popover top-full right-0 mt-1 flex w-105 flex-col">
			<div class="flex items-center justify-between border-b border-line px-4 py-3">
				<span class="large">Review changes</span>
				<span class="badge {config.errors.length ? 'badge-error' : 'badge-warning'}">{summary}</span
				>
			</div>
			{@render body()}
			<div class="dialog-footer">{@render buttons()}</div>
		</div>
	{/if}
</div>
{#if open && media.compact}
	<Sheet title="Review changes" subtitle={summary} onclose={() => (open = false)} footer={buttons}>
		{@render body()}
	</Sheet>
{/if}
