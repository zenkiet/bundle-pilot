<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { create } from '@bufbuild/protobuf';
	import { Check, ChevronRight, CircleCheck, Eye, EyeOff } from '@lucide/svelte';
	import { AuthSchema } from '$lib/entities/config/gen/edgegateway/config/v1/config_pb';
	import { config } from '$lib/entities/config/store.svelte';
	import { status } from '$lib/entities/status/store.svelte';
	import AppearanceSelector from '$lib/features/toggle-theme/AppearanceSelector.svelte';
	import { ago, mb, message } from '$lib/shared/lib/api';
	import Banner from '$lib/shared/ui/Banner.svelte';
	import Selector from '$lib/shared/ui/Selector.svelte';
	import { toast } from '$lib/shared/ui/Toast.svelte';
	import GeneralCard from '$lib/widgets/general/GeneralCard.svelte';
	import SourceCard from '$lib/widgets/source/SourceCard.svelte';

	const steps = ['Project', 'Admin account', 'Bundle source', 'Default bundle', 'Review'] as const;

	let step = $state(1);
	let user = $state('admin');
	let password = $state('');
	let confirm = $state('');
	let show = $state(false);
	let busy = $state(false);
	const d = $derived(config.draft);
	const s = $derived(status.value);
	const bundles = $derived(
		(s?.bundles ?? []).map((b) => ({
			value: b.version,
			label: b.version,
			desc: `${mb(b.zip_bytes)} · ${ago(b.mod_time)}`
		}))
	);
	const pwOk = $derived(password.length >= 8 && password === confirm);
	const ready = $derived([!!d.projectName.trim(), pwOk, true, true, true][step - 1]);

	$effect(() => {
		d.auth = pwOk ? create(AuthSchema, { username: user.trim(), password }) : undefined;
	});

	async function finish() {
		busy = true;
		try {
			const r = await config.save();
			if (!r.saved) return toast(r.errors[0] ?? 'Not saved', { error: true });
			toast(`config.pb written. Signed in as ${user}`);
			await goto(resolve('/'));
		} catch (e) {
			toast(message(e), { error: true });
		} finally {
			busy = false;
		}
	}
</script>

<div class="flex min-h-dvh flex-col">
	<header class="flex min-h-12 items-center gap-4 border-b border-line px-4 py-2 md:px-6">
		<div class="flex flex-col">
			<span class="large">Edge gateway</span><span class="sup"
				>First-run setup · config.pb not found</span
			>
		</div>
		<div class="ml-auto hidden items-center gap-2 md:flex">
			<AppearanceSelector size="sm" />
		</div>
	</header>
	<main class="flex flex-1 justify-center p-3 md:p-10">
		<div class="card w-full max-w-190 gap-0 self-start p-0">
			<div class="flex flex-col gap-4 border-b border-line px-4 pt-4 pb-3 md:px-6 md:pt-5">
				<div class="flex flex-col">
					<h1 class="text-xl leading-7 font-semibold">Set up the gateway</h1>
					<span class="sup">Five steps. Everything can be changed later in Settings.</span>
				</div>
				<div class="steps hidden md:flex">
					{#each steps as label, i (label)}
						<div class="step {i + 1 < step ? 'step-done' : i + 1 === step ? 'step-on' : ''}">
							<span class="step-n"
								>{#if i + 1 < step}<Check size={12} />{:else}{i + 1}{/if}</span
							><span class="step-l">{label}</span>
						</div>
						{#if i < steps.length - 1}<div class="step-line"></div>{/if}
					{/each}
				</div>
				<div class="flex flex-col gap-2 md:hidden">
					<div class="progress">
						<div class="track">
							<div class="fill" style="width: {(step / steps.length) * 100}%"></div>
						</div>
					</div>
					<span class="sup">Step {step} of {steps.length} · {steps[step - 1]}</span>
				</div>
			</div>

			<div class="flex min-h-80 flex-col gap-4 px-4 py-5 md:px-6">
				{#if step === 1}
					<Banner
						status="info"
						title="Nothing is configured yet"
						desc="Until setup finishes every visitor gets the newest bundle on disk and the admin API has no password."
					/>
					<GeneralCard />
				{:else if step === 2}
					<div class="grid gap-4 md:grid-cols-2">
						<label class="field"
							><span class="field-label">Username</span><input
								class="input input-lg mono"
								bind:value={user}
								autocomplete="username"
							/></label
						>
						<div class="hidden md:block"></div>
						<label class="field"
							><span class="field-label">Password</span><span class="input input-lg pr-1"
								><input
									class="mono min-w-0 flex-1 bg-transparent outline-none"
									type={show ? 'text' : 'password'}
									bind:value={password}
									autocomplete="new-password"
								/><button
									type="button"
									class="btn btn-ghost btn-sm btn-icon"
									aria-label="Show password"
									onclick={() => (show = !show)}
									>{#if show}<EyeOff size={16} />{:else}<Eye size={16} />{/if}</button
								></span
							><span class="field-desc">At least 8 characters.</span></label
						>
						<label class="field"
							><span class="field-label">Confirm password</span><input
								class="input input-lg mono {confirm && confirm !== password ? 'input-error' : ''}"
								type={show ? 'text' : 'password'}
								bind:value={confirm}
								autocomplete="new-password"
							/></label
						>
					</div>
					<Banner
						status="info"
						title="Stored as a salted PBKDF2-SHA256 hash inside config.pb"
						desc="Sent as HTTP Basic auth to the admin API, never to /__gateway/data. Put the admin behind TLS."
					/>
				{:else if step === 3}
					<SourceCard />
				{:else if step === 4}
					<div class="field md:w-90">
						<span class="field-label">Default bundle</span><Selector
							title="Default bundle"
							options={bundles}
							bind:value={d.defaultBundle}
							placeholder="Newest bundle"
							mono
							size="lg"
						/><span class="field-desc"
							>Served when no rule or backend key matches. {bundles.length
								? ''
								: 'No bundles on disk yet: upload after setup.'}</span
						>
					</div>
					<dl class="meta">
						<dt>Signing</dt>
						<dd class="flex items-center gap-2">
							<span class="badge {s?.signing ? 'badge-green' : ''}"
								>{s?.signing ? 'on' : 'off'}</span
							><span class="sup">set by BUNDLE_PUBKEY in the environment</span>
						</dd>
						<dt>Rules</dt>
						<dd class="sup">Add rules and the backend table from Overview after setup.</dd>
					</dl>
				{:else}
					<dl class="meta gap-y-2.5">
						<dt>Project</dt>
						<dd>
							{d.projectName}{#if d.environment}<span class="sup ml-1">· {d.environment}</span>{/if}
						</dd>
						<dt>Date format</dt>
						<dd class="mono">{d.dateFormat || 'MM.dd.YYYY'}</dd>
						<dt>Admin</dt>
						<dd class="flex items-center gap-2">
							<span class="mono">{user}</span><span class="badge badge-green">password set</span>
						</dd>
						<dt>Source</dt>
						<dd>
							<span class="token mono"
								>{d.source?.type === 's3'
									? `s3://${d.source.bucket}/${d.source.prefix}`
									: 'local · versions/'}</span
							>
						</dd>
						<dt>Default bundle</dt>
						<dd class="mono">{d.defaultBundle || 'newest'}</dd>
					</dl>
					{#if config.errors.length}<Banner status="error" title={config.errors[0]} />{:else}<Banner
							status="warning"
							title="The admin API is open until you finish"
							desc="Finishing writes config.pb, reloads the gateway and signs you in."
						/>{/if}
				{/if}
			</div>

			<div class="dialog-footer justify-between">
				<span class="sup">Step {step} of {steps.length}</span>
				<span class="flex gap-2">
					{#if step > 1}<button class="btn" onclick={() => step--}>Back</button>{/if}
					{#if step < steps.length}<button
							class="btn btn-primary"
							onclick={() => step++}
							disabled={!ready || !user.trim()}>Continue<ChevronRight size={16} /></button
						>
					{:else}<button
							class="btn btn-primary"
							onclick={finish}
							disabled={busy || config.errors.length > 0}
							><CircleCheck size={16} />Finish setup</button
						>{/if}
				</span>
			</div>
		</div>
	</main>
</div>
