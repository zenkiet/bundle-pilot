<script lang="ts">
	import './layout.css';
	import favicon from '$lib/assets/favicon.svg';
	import { toBinary } from '@bufbuild/protobuf';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { ListChecks, Package, RefreshCw, Save, SunMoon, Undo2, Upload } from '@lucide/svelte';
	import { reloadGateway } from '$lib/entities/bundle/api';
	import { ConfigSchema } from '$lib/entities/config/gen/bundlepilot/config/v1/config_pb';
	import { config } from '$lib/entities/config/store.svelte';
	import { status } from '$lib/entities/status/store.svelte';
	import CommandPalette, {
		type Command
	} from '$lib/features/command-palette/CommandPalette.svelte';
	import { saveAll } from '$lib/features/save-config/save';
	import { theme } from '$lib/features/toggle-theme/theme.svelte';
	import { message, unauthorized } from '$lib/shared/lib/api';
	import Toast, { toast } from '$lib/shared/ui/Toast.svelte';
	import AppShell from '$lib/widgets/app-shell/AppShell.svelte';
	import { jump } from '$lib/widgets/app-shell/nav.svelte';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';

	let { children } = $props();
	const bare = $derived(/\/(setup|login)\/?$/.test(page.url.pathname));
	let palette = $state<CommandPalette | null>(null);

	onMount(() => {
		const t = setInterval(() => status.refresh(), 5000);
		return () => clearInterval(t);
	});
	$effect(() => {
		if (bare && !page.url.pathname.includes('/setup')) return;
		status.refresh().then(() => {
			const setup = page.url.pathname.includes('/setup');
			if (status.value && status.value.setup_required !== setup)
				return goto(resolve(status.value.setup_required ? '/setup' : '/'));
			config
				.load()
				.catch(
					(e) => e !== unauthorized && toast(`Cannot load config: ${message(e)}`, { error: true })
				);
		});
	});

	let timer: ReturnType<typeof setTimeout>;
	$effect(() => {
		if (!config.loaded) return;
		toBinary(ConfigSchema, config.draft);
		clearTimeout(timer);
		if (config.dirty)
			timer = setTimeout(
				() => config.validate().catch((e) => toast(message(e), { error: true })),
				400
			);
	});

	const commands = $derived<Command[]>([
		{
			id: 'save',
			group: 'Actions',
			label: 'Save and reload',
			hint: '⌘S',
			icon: Save,
			run: saveAll
		},
		{
			id: 'discard',
			group: 'Actions',
			label: 'Discard changes',
			icon: Undo2,
			run: () => config.reset()
		},
		{
			id: 'upload',
			group: 'Actions',
			label: 'Upload bundle',
			icon: Upload,
			run: () => goto(resolve('/upload'))
		},
		{
			id: 'reload',
			group: 'Actions',
			label: 'Reload config from disk',
			icon: RefreshCw,
			run: () =>
				reloadGateway().then(
					(r) => {
						toast(`Reloaded in ${r.reload_ms} ms`);
						status.refresh();
					},
					(e) => toast(message(e), { error: true })
				)
		},
		{
			id: 'theme',
			group: 'Actions',
			label: 'Cycle appearance',
			icon: SunMoon,
			run: () => theme.cycle()
		},
		...config.draft.rules.map((r, i) => ({
			id: `rule-${i}`,
			group: 'Rules',
			label: r.id || `Rule ${i + 1}`,
			hint: r.bundle,
			icon: ListChecks,
			run: () => jump('rules')
		})),
		...(status.value?.bundles ?? []).map((b) => ({
			id: `b-${b.version}`,
			group: 'Bundles',
			label: b.version,
			hint: b.version === status.value?.default ? 'default' : undefined,
			icon: Package,
			run: () => jump('bundles')
		}))
	]);

	function key(e: KeyboardEvent) {
		if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 's') {
			e.preventDefault();
			saveAll();
		}
	}
</script>

<svelte:head
	><link rel="icon" href={favicon} /><title>{status.value?.project_name || 'Bundle Pilot'}</title
	></svelte:head
>
<svelte:window onkeydown={key} />

{#if bare}
	{@render children()}
{:else}
	<AppShell onsearch={() => palette?.show()}>
		{@render children()}
	</AppShell>
{/if}

<CommandPalette bind:this={palette} {commands} />
<Toast />
