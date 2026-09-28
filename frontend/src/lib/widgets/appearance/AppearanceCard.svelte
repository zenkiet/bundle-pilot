<script lang="ts">
	import { status } from '$lib/entities/status/store.svelte';
	import AppearanceSelector from '$lib/features/toggle-theme/AppearanceSelector.svelte';
	import { ago } from '$lib/shared/lib/api';
	import Card from '$lib/shared/ui/Card.svelte';
</script>

<Card title="Appearance" class="md:hidden">
	<div class="field"><span class="field-label">Theme</span><AppearanceSelector size="lg" /></div>
	<dl class="meta">
		<dt>Gateway</dt>
		<dd class="mono">{status.value?.version ?? '…'}</dd>
		<dt>Started</dt>
		<dd>{status.value ? ago(status.value.started_at) : '…'}</dd>
	</dl>
</Card>
