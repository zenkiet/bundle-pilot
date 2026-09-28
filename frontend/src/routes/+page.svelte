<script lang="ts">
	import DecisionTester from '$lib/features/test-decision/DecisionTester.svelte';
	import TabList from '$lib/shared/ui/TabList.svelte';
	import BackendCard from '$lib/widgets/backend/BackendCard.svelte';
	import BundlesCard from '$lib/widgets/bundles/BundlesCard.svelte';
	import DecisionsCard from '$lib/widgets/decisions/DecisionsCard.svelte';
	import DefaultBundleCard from '$lib/widgets/default-bundle/DefaultBundleCard.svelte';
	import HealthCard from '$lib/widgets/health/HealthCard.svelte';
	import IssuesBanner from '$lib/widgets/issues/IssuesBanner.svelte';
	import RulesCard from '$lib/widgets/rules/RulesCard.svelte';

	import { nav, type Tab } from '$lib/widgets/app-shell/nav.svelte';
	const tabs: { value: Tab; label: string }[] = [
		{ value: 'overview', label: 'Overview' },
		{ value: 'rules', label: 'Rules' },
		{ value: 'bundles', label: 'Bundles' }
	];
	if (!tabs.some((t) => t.value === nav.tab)) nav.tab = 'overview';
</script>

<TabList bind:value={nav.tab} {tabs} fill class="sticky top-0 z-10 bg-bg px-2 pt-1 md:hidden" />
<div
	class="grid grid-cols-1 gap-3 p-3 md:grid-cols-2 md:gap-4 md:p-4 lg:grid-cols-12"
	data-tab={nav.tab}
>
	<div class="empty:hidden md:col-span-2 lg:col-span-12" data-sec="overview"><IssuesBanner /></div>
	<div class="contents" data-sec="overview"><HealthCard /></div>
	<div class="contents" data-sec="overview"><DefaultBundleCard /></div>
	<div class="contents" data-sec="overview"><DecisionsCard /></div>
	<div class="contents" data-sec="rules"><RulesCard /></div>
	<div class="contents" data-sec="bundles"><BundlesCard /></div>
	<div class="contents" data-sec="rules"><BackendCard /></div>
	<div class="contents" data-sec="overview"><DecisionTester /></div>
</div>
