<script lang="ts">
	import { config } from '$lib/entities/config/store.svelte';
	import { diff } from '$lib/features/save-config/diff';
	import ReviewChanges from '$lib/features/save-config/ReviewChanges.svelte';
	import { saveAll } from '$lib/features/save-config/save';

	const n = $derived(diff(config.saved, config.draft).length);
</script>

{#if config.dirty}
	<div
		class="flex items-center gap-2 border-t border-line bg-surface px-3 pt-2 pb-[max(env(safe-area-inset-bottom),12px)]"
		role="toolbar"
		aria-label="Unsaved changes"
	>
		<div class="flex min-w-0 flex-1 flex-col">
			<span class="truncate font-medium">{n} unsaved change{n === 1 ? '' : 's'}</span>
			<span class="sup"
				>{config.errors.length
					? `${config.errors.length} errors`
					: `${config.issues.length} warnings`}</span
			>
		</div>
		<ReviewChanges />
		<button
			class="btn btn-primary"
			onclick={saveAll}
			disabled={config.busy || config.errors.length > 0}>Save</button
		>
	</div>
{/if}
