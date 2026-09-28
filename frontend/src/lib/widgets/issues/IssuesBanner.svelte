<script lang="ts">
	import { config } from '$lib/entities/config/store.svelte';
	import { status } from '$lib/entities/status/store.svelte';
	import Banner from '$lib/shared/ui/Banner.svelte';

	const errors = $derived([
		...config.errors,
		...(status.value?.errors ?? []),
		...(status.value?.last_error ? [status.value.last_error] : [])
	]);
	const warnings = $derived(config.dirty ? config.issues : (status.value?.issues ?? []));
	const n = (k: number, w: string) => `${k} ${w}${k === 1 ? '' : 's'}`;
</script>

{#if errors.length}
	<Banner
		status="error"
		title={n(errors.length, 'error') + (config.dirty ? ' in the draft' : ' in the loaded config')}
		desc={errors.join(' · ')}
	/>
{:else if warnings.length}
	<Banner
		status="warning"
		title={n(warnings.length, 'warning') +
			(config.dirty ? ' in the draft' : ' in the current config')}
		desc={warnings.join(' · ')}
	/>
{/if}
