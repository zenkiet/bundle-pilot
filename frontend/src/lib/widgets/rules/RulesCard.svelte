<script lang="ts">
	import { ArrowDown, ArrowUp, Copy, Pencil, Plus, Trash2 } from '@lucide/svelte';
	import { clone } from '@bufbuild/protobuf';
	import { RuleSchema, type Rule } from '$lib/entities/config/gen/edgegateway/config/v1/config_pb';
	import { config } from '$lib/entities/config/store.svelte';
	import { status } from '$lib/entities/status/store.svelte';
	import { toRows } from '$lib/entities/config/when';
	import RuleDialog from '$lib/features/edit-rule/RuleDialog.svelte';
	import Card from '$lib/shared/ui/Card.svelte';
	import Menu from '$lib/shared/ui/Menu.svelte';

	let open = $state(false);
	let editing = $state(-1);
	const bundles = $derived(status.value?.bundles.map((b) => b.version) ?? []);
	const rules = $derived(config.draft.rules);
	const set = (next: Rule[]) => (config.draft.rules = next);
	const stateOf = (r: Rule) =>
		!bundles.includes(r.bundle)
			? ['', 'inactive', 'bundle not on disk']
			: r.until && Date.parse(r.until) < Date.now()
				? ['', 'expired', `until ${r.until}`]
				: ['badge-success', 'active', r.until ? `until ${r.until}` : r.note || 'no expiry'];

	function edit(i: number) {
		editing = i;
		open = true;
	}
	function apply(r: Rule) {
		const next = [...rules];
		if (editing < 0) next.push(r);
		else next.splice(editing, 1, r);
		set(next);
	}
	function move(i: number, d: number) {
		const next = [...rules];
		const [r] = next.splice(i, 1);
		next.splice(i + d, 0, r);
		set(next);
	}
	const remove = (i: number) => set(rules.filter((_, j) => j !== i));
	const duplicate = (i: number) =>
		set([
			...rules.slice(0, i + 1),
			clone(RuleSchema, { ...rules[i], id: `${rules[i].id}-copy` }),
			...rules.slice(i + 1)
		]);
</script>

<Card title="Rules" hint="first match wins" id="rules" class="gap-2 md:col-span-2 lg:col-span-8">
	{#snippet actions()}
		<button class="btn btn-sm" onclick={() => edit(-1)}><Plus size={16} />New rule</button>
	{/snippet}
	{#if rules.length === 0}
		<div class="empty empty-compact rounded-container border border-dashed border-line-2">
			<span class="font-medium">No rules yet</span><span class="sup"
				>Everyone gets the backend table, then the default bundle.</span
			>
		</div>
	{:else}
		<div
			class="hidden grid-cols-[32px_150px_1fr_96px_88px_40px] items-center gap-x-3 border-b border-line px-2 pb-2 text-sm font-semibold text-fg-2 md:grid"
		>
			<span>#</span><span>Rule</span><span>When</span><span>Serve</span><span>Status</span><span
			></span>
		</div>
		<div class="flex flex-col md:gap-0">
			{#each rules as r, i (i)}
				{@const [cls, text, meta] = stateOf(r)}
				<div
					class="grid grid-cols-[1fr_auto_auto] items-center gap-x-2 gap-y-2 border-b border-line px-2 py-2.5 last:border-b-0 md:grid-cols-[32px_150px_1fr_96px_88px_40px] md:gap-x-3 md:hover:bg-hover"
				>
					<span class="mono hidden text-fg-2 md:inline">{i + 1}</span>
					<button class="flex min-w-0 flex-col text-left" onclick={() => edit(i)}>
						<span class="truncate font-medium">{r.id || `#${i + 1}`}</span><span
							class="desc truncate">{meta}</span
						>
					</button>
					<div
						class="order-last col-span-3 flex flex-wrap items-center gap-1 md:order-0 md:col-span-1"
					>
						{#each toRows(r.when) as w (w.fact)}<span class="token mono">{w.fact} = {w.value}</span
							>{/each}
						<span class="sup md:hidden">→</span><span class="token token-green mono md:hidden"
							>{r.bundle}</span
						>
					</div>
					<span class="token token-green mono hidden md:inline-block">{r.bundle}</span>
					<span class="badge {cls}">{text}</span>
					<Menu
						title={r.id || `Rule ${i + 1}`}
						subtitle="Rule {i + 1} of {rules.length} · serves {r.bundle}"
						items={[
							{ label: 'Edit', icon: Pencil, kbd: '↵', run: () => edit(i) },
							{ label: 'Duplicate', icon: Copy, run: () => duplicate(i) },
							{ label: 'Move up', icon: ArrowUp, disabled: i === 0, run: () => move(i, -1) },
							{
								label: 'Move down',
								icon: ArrowDown,
								disabled: i === rules.length - 1,
								run: () => move(i, 1)
							},
							{
								label: 'Delete rule',
								icon: Trash2,
								destructive: true,
								divider: true,
								run: () => remove(i)
							}
						]}
					/>
				</div>
			{/each}
		</div>
	{/if}
</Card>

<RuleDialog
	bind:open
	rule={editing >= 0 ? (rules[editing] ?? null) : null}
	position={editing >= 0 ? editing + 1 : rules.length + 1}
	{bundles}
	onsave={apply}
	ondelete={editing >= 0 ? () => remove(editing) : undefined}
/>
