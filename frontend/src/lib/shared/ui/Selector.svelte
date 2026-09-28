<script lang="ts" module>
	export interface Option<T extends string> {
		value: T;
		label: string;
		desc?: string;
		badge?: string;
	}
</script>

<script lang="ts" generics="T extends string">
	import { Check, ChevronDown } from '@lucide/svelte';
	import { clickOutside } from '$lib/shared/lib/actions';
	import { media } from '$lib/shared/lib/media.svelte';
	import Sheet from './Sheet.svelte';

	let {
		value = $bindable(),
		options,
		title,
		placeholder = 'Choose',
		size = 'md',
		mono = false,
		ghost = false,
		onchange
	}: {
		value?: T;
		options: Option<T>[];
		title: string;
		placeholder?: string;
		size?: 'sm' | 'md' | 'lg';
		mono?: boolean;
		ghost?: boolean;
		onchange?: (v: T) => void;
	} = $props();

	let open = $state(false);
	let up = $state(false);
	let root: HTMLDivElement;
	const current = $derived(options.find((o) => o.value === value));

	function pick(v: T) {
		value = v;
		open = false;
		onchange?.(v);
	}
</script>

{#snippet list(lg: boolean)}
	{#each options as o (o.value)}
		<button
			type="button"
			class="option {lg ? 'option-lg' : ''} {o.value === value ? 'option-sel' : ''}"
			role="option"
			aria-selected={o.value === value}
			onclick={() => pick(o.value)}
		>
			<span class="flex min-w-0 flex-col">
				<span class:mono>{o.label}</span>
				{#if o.desc}<span class="desc">{o.desc}</span>{/if}
			</span>
			<span class="flex items-center gap-2">
				{#if o.badge}<span class="badge badge-green">{o.badge}</span>{/if}
				{#if o.value === value}<Check size={16} class="check" />{/if}
			</span>
		</button>
	{/each}
{/snippet}

<div
	class="relative min-w-0"
	bind:this={root}
	use:clickOutside={() => !media.compact && (open = false)}
>
	<button
		type="button"
		class="input selector input-{size} {open ? 'selector-open input-focus' : ''} {ghost
			? 'selector-ghost'
			: ''}"
		aria-haspopup="listbox"
		aria-expanded={open}
		aria-label={title}
		onclick={() => {
			up = root.getBoundingClientRect().bottom > innerHeight / 2;
			open = !open;
		}}
	>
		<span class="val">
			{#if current}
				<span class:mono class="truncate">{current.label}</span>
				{#if current.desc}<span class="sup hidden sm:inline">{current.desc}</span>{/if}
			{:else}<span class="ph">{placeholder}</span>{/if}
		</span>
		<span class="chev"><ChevronDown size={16} /></span>
	</button>
	{#if open && !media.compact}
		<div
			class="popover listbox right-0 left-0 {up ? 'bottom-full mb-1' : 'top-full mt-1'}"
			role="listbox"
		>
			{@render list(false)}
		</div>
	{/if}
</div>
{#if open && media.compact}
	<Sheet {title} onclose={() => (open = false)}>
		<div class="listbox max-h-none p-0" role="listbox">{@render list(true)}</div>
	</Sheet>
{/if}
