<script lang="ts" generics="T extends string">
	let {
		value = $bindable(),
		tabs,
		fill = false,
		class: cls = ''
	}: { value?: T; tabs: { value: T; label: string }[]; fill?: boolean; class?: string } = $props();
</script>

<div class="tabs tabs-divider {fill ? 'tabs-fill' : ''} {cls}" role="tablist">
	{#each tabs as t (t.value)}
		<button
			type="button"
			class="tab {t.value === value ? 'tab-on' : ''}"
			role="tab"
			aria-selected={t.value === value}
			onclick={() => (value = t.value)}>{t.label}</button
		>
	{/each}
</div>
