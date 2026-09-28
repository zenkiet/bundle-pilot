<script lang="ts">
	let {
		checked = $bindable(false),
		label,
		desc
	}: { checked?: boolean; label: string; desc?: string } = $props();
</script>

<label class="flex cursor-pointer items-center gap-2 py-1">
	<button
		type="button"
		role="switch"
		aria-checked={checked}
		aria-label={label}
		class="switch {checked ? 'switch-on' : ''}"
		onclick={() => (checked = !checked)}
	></button>
	<span class="flex min-w-0 flex-col"
		><span>{label}</span>{#if desc}<span class="sup">{desc}</span>{/if}</span
	>
</label>
