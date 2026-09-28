<script lang="ts">
	import { page } from '$app/state';
	import { resolve } from '$app/paths';
	import logo from '$lib/assets/favicon.svg';
	import { config } from '$lib/entities/config/store.svelte';
	import { status } from '$lib/entities/status/store.svelte';
	import { ago } from '$lib/shared/lib/api';
	import AppearanceSelector from '$lib/features/toggle-theme/AppearanceSelector.svelte';
	import { jump, nav, pages, sections, type Path } from './nav.svelte';

	let { rail = false, onnavigate }: { rail?: boolean; onnavigate?: () => void } = $props();
	const counts = $derived({
		rules: config.draft.rules.length,
		bundles: status.value?.bundles.length ?? 0
	});
	const s = $derived(status.value);
	const here = (path: Path) =>
		page.url.pathname.replace(/\/$/, '') === resolve(path).replace(/\/$/, '');
	const healthy = $derived(!!s && !s.last_error && s.errors.length === 0);
</script>

<nav class="flex h-full flex-col {rail ? 'w-12 items-center' : 'w-full lg:w-65'}" aria-label="Main">
	{#if rail}
		<img src={logo} alt="" class="mt-3 mb-1 size-8" />
	{:else}
		<div class="flex items-center gap-3 px-4 pt-3 pb-2">
			<img src={logo} alt="" class="size-10 flex-none" />
			<div class="min-w-0">
				<div class="large truncate">
					{s?.project_name || 'Edge gateway'}{#if s?.environment}<span
							class="badge ml-2 align-middle">{s.environment}</span
						>{/if}
				</div>
				<div class="sup truncate">{s?.base_path ?? '/'} · {s?.default ?? '…'}</div>
			</div>
		</div>
	{/if}
	<div class="flex min-h-0 flex-1 flex-col gap-0.5 overflow-auto px-2 py-2">
		{#if !rail}<div class="nav-section-title">Configure</div>{/if}
		{#each pages as p (p.path)}
			<a
				class="nav-item {rail ? 'nav-icon' : ''} {here(p.path) && !nav.section ? 'nav-on' : ''}"
				href={resolve(p.path)}
				aria-label={p.label}
				title={rail ? p.label : undefined}
				onclick={(e) => {
					onnavigate?.();
					e.preventDefault();
					jump(null, p.path);
				}}
			>
				<p.icon size={16} />
				{#if !rail}<span class="grow-1">{p.label}</span>{/if}
			</a>
			{#if !rail && here(p.path)}
				{#each sections.filter((s) => s.path === p.path) as sec (sec.id)}
					<button
						class="nav-item h-7 pl-8 {nav.section === sec.id ? 'nav-on' : ''}"
						aria-current={nav.section === sec.id ? 'location' : undefined}
						onclick={() => {
							onnavigate?.();
							jump(sec.id);
						}}
					>
						<sec.icon size={14} />
						<span class="grow-1">{sec.label}</span>
						{#if sec.badge}<span class="badge">{sec.badge(counts)}</span>{/if}
					</button>
				{/each}
			{/if}
		{/each}
	</div>
	<div class="flex flex-col gap-1 px-2 pb-2 {rail ? 'items-center' : ''}">
		<div
			class="nav-item cursor-default {rail ? 'nav-icon' : ''}"
			title={healthy ? 'Healthy' : 'Degraded'}
		>
			<span class="dot {healthy ? 'dot-success' : 'dot-warning'}"></span>
			{#if !rail}<span class="grow-1">{healthy ? 'Healthy' : 'Degraded'}</span><span class="sup"
					>{s ? ago(s.loaded_at) : ''}</span
				>{/if}
		</div>
		{#if !rail}
			<dl class="meta px-2 pb-1 text-xs">
				<dt class="text-xs">Reloads</dt>
				<dd class="text-xs">{s?.reloads ?? 0}</dd>
				<dt class="text-xs">Signing</dt>
				<dd>
					<span class="badge {s?.signing ? 'badge-green' : ''}">{s?.signing ? 'on' : 'off'}</span>
				</dd>
			</dl>
			<AppearanceSelector size="sm" />
		{/if}
	</div>
</nav>
