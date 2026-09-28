<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { Eye, EyeOff } from '@lucide/svelte';
	import { status } from '$lib/entities/status/store.svelte';
	import AppearanceSelector from '$lib/features/toggle-theme/AppearanceSelector.svelte';
	import { session } from '$lib/shared/lib/session.svelte';
	import Banner from '$lib/shared/ui/Banner.svelte';

	let user = $state('admin');
	let password = $state('');
	let show = $state(false);
	let failed = $state(false);
	let busy = $state(false);

	async function signIn(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		session.set(user, password);
		await status.refresh();
		busy = false;
		if (status.error) {
			session.clear();
			failed = true;
			return;
		}
		await goto(resolve('/'));
	}
</script>

<div class="flex min-h-dvh items-center justify-center p-4">
	<div class="absolute top-3 right-4 hidden md:block"><AppearanceSelector size="sm" /></div>
	<form class="card w-full max-w-100 gap-4 p-5 md:p-6" onsubmit={signIn}>
		<div class="flex flex-col">
			<h1 class="text-xl leading-7 font-semibold">Sign in</h1>
			<span class="sup">{status.value?.project_name || 'Bundle Pilot'} · admin</span>
		</div>
		{#if failed}<Banner status="error" title="Wrong username or password" />{/if}
		<label class="field"
			><span class="field-label">Username</span><input
				class="input input-lg mono"
				bind:value={user}
				autocomplete="username"
			/></label
		>
		<label class="field"
			><span class="field-label">Password</span>
			<span class="input input-lg pr-1"
				><input
					class="mono min-w-0 flex-1 bg-transparent outline-none"
					type={show ? 'text' : 'password'}
					bind:value={password}
					autocomplete="current-password"
				/><button
					type="button"
					class="btn btn-ghost btn-sm btn-icon"
					aria-label="Show password"
					onclick={() => (show = !show)}
					>{#if show}<EyeOff size={16} />{:else}<Eye size={16} />{/if}</button
				></span
			>
		</label>
		<button class="btn btn-primary btn-lg w-full" type="submit" disabled={busy || !password}
			>Sign in</button
		>
		<span class="sup text-center">Kept for this tab only · HTTP Basic auth</span>
	</form>
</div>
