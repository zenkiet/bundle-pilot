import { config } from '$lib/entities/config/store.svelte';
import { status } from '$lib/entities/status/store.svelte';
import { toast } from '$lib/shared/ui/Toast.svelte';
import { message } from '$lib/shared/lib/api';

export async function saveAll() {
	if (!config.dirty || config.busy || config.errors.length) return;
	try {
		const r = await config.save();
		if (r.saved) {
			toast('Saved. The gateway reloaded.');
			await status.refresh();
		} else toast(r.errors[0] ?? 'Not saved', { error: true });
	} catch (e) {
		toast(message(e), { error: true });
	}
}
