<script lang="ts">
	import TabList from '$lib/shared/ui/TabList.svelte';
	import { nav, type Tab } from '$lib/widgets/app-shell/nav.svelte';
	import AppearanceCard from '$lib/widgets/appearance/AppearanceCard.svelte';
	import DefaultBundleCard from '$lib/widgets/default-bundle/DefaultBundleCard.svelte';
	import GatewayCard from '$lib/widgets/gateway/GatewayCard.svelte';
	import GeneralCard from '$lib/widgets/general/GeneralCard.svelte';
	import SecurityCard from '$lib/widgets/security/SecurityCard.svelte';
	import SourceCard from '$lib/widgets/source/SourceCard.svelte';

	const tabs: { value: Tab; label: string }[] = [
		{ value: 'general', label: 'General' },
		{ value: 'account', label: 'Account' },
		{ value: 'source', label: 'Source' },
		{ value: 'default', label: 'Default' }
	];
	if (!tabs.some((t) => t.value === nav.tab)) nav.tab = 'general';
</script>

<TabList bind:value={nav.tab} {tabs} fill class="sticky top-0 z-10 bg-bg px-2 pt-1 md:hidden" />
<div
	class="grid grid-cols-1 gap-3 p-3 md:grid-cols-2 md:gap-4 md:p-4 lg:grid-cols-12"
	data-tab={nav.tab}
>
	<div class="contents" data-sec="general"><GeneralCard /></div>
	<div class="contents" data-sec="general"><GatewayCard /></div>
	<div class="contents" data-sec="account"><SecurityCard /></div>
	<div class="contents" data-sec="source"><SourceCard /></div>
	<div class="contents" data-sec="default"><DefaultBundleCard /></div>
	<div class="contents" data-sec="general"><AppearanceCard /></div>
</div>
