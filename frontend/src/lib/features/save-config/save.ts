import { stale } from '#lib/entities/config/api.ts';
import { config } from '#lib/entities/config/store.svelte.ts';
import { status } from '#lib/entities/status/store.svelte.ts';
import { toast } from '#lib/shared/ui/Toast.svelte';
import { message } from '#lib/shared/lib/api.ts';

export async function saveAll() {
	if (!config.dirty || config.busy || config.errors.length) return;
	try {
		const r = await config.save();
		if (r.saved) {
			toast('Saved. The gateway reloaded.');
			await status.refresh();
		} else toast(r.errors[0] ?? 'Not saved', { error: true });
	} catch (e) {
		toast(message(e), { error: true, action: e === stale ? reload : undefined });
	}
}

const reload = {
	label: 'Reload',
	run: () => void config.load().catch((e) => toast(message(e), { error: true }))
};
