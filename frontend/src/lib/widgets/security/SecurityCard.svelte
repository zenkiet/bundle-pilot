<script lang="ts">
	import { create } from '@bufbuild/protobuf';
	import { AuthSchema } from '$lib/entities/config/gen/bundlepilot/config/v1/config_pb';
	import { config } from '$lib/entities/config/store.svelte';
	import Card from '$lib/shared/ui/Card.svelte';

	let user = $derived(config.saved.auth?.username ?? 'admin');
	let password = $state('');
	let confirm = $state('');
	const ok = $derived(password.length >= 8 && password === confirm);
	$effect(() => {
		const cur = config.saved.auth;
		config.draft.auth =
			ok || cur
				? create(AuthSchema, {
						username: user.trim(),
						passwordHash: cur?.passwordHash ?? '',
						password: ok ? password : ''
					})
				: undefined;
	});
</script>

<Card
	title="Admin account"
	hint="HTTP Basic auth on the admin API"
	id="account"
	class="lg:col-span-4"
>
	<label class="field"
		><span class="field-label">Username</span><input
			class="input mono"
			bind:value={user}
			autocomplete="username"
		/></label
	>
	<div class="grid grid-cols-2 gap-2">
		<label class="field"
			><span class="field-label">New password</span><input
				class="input mono"
				type="password"
				bind:value={password}
				placeholder={config.saved.auth ? 'unchanged' : 'none yet'}
				autocomplete="new-password"
			/></label
		>
		<label class="field"
			><span class="field-label">Confirm</span><input
				class="input mono {confirm && confirm !== password ? 'input-error' : ''}"
				type="password"
				bind:value={confirm}
				autocomplete="new-password"
			/></label
		>
	</div>
	<span class="sup"
		>Stored as a PBKDF2-SHA256 hash; this tab signs in with the new password on Save.</span
	>
</Card>
