import { uploadBundle } from '#lib/entities/bundle/api.ts';
import { config } from '#lib/entities/config/store.svelte.ts';
import { status } from '#lib/entities/status/store.svelte.ts';
import { message } from '#lib/shared/lib/api.ts';
import { toast } from '#lib/shared/ui/Toast.svelte';

export type ItemStatus = 'skipped' | 'queued' | 'uploading' | 'verified' | 'error';

interface QueueItem {
	id: number;
	file: File;
	status: ItemStatus;
	pct: number;
	note: string;
}

let seq = 0;

class UploadQueue {
	items = $state<QueueItem[]>([]);
	running = $state(false);
	setDefault = $state(false);
	queued = $derived(this.items.filter((i) => i.status === 'queued').length);
	verified = $derived(this.items.filter((i) => i.status === 'verified').length);
	skipped = $derived(this.items.filter((i) => i.status === 'skipped'));

	add(files: Iterable<File>) {
		for (const file of files) {
			const zip = file.name.toLowerCase().endsWith('.zip');
			this.items = this.items.filter((i) => i.file.name !== file.name || i.status === 'verified');
			this.items.push({
				id: ++seq,
				file,
				status: zip ? 'queued' : 'skipped',
				pct: 0,
				note: zip ? 'Queued' : 'Only .zip files are accepted'
			});
		}
	}

	remove(id: number) {
		this.items = this.items.filter((i) => i.id !== id);
	}

	retry(id: number) {
		const it = this.items.find((i) => i.id === id);
		if (!it) return;
		it.status = 'queued';
		it.note = 'Queued';
		it.pct = 0;
	}

	clearDone() {
		this.items = this.items.filter((i) => i.status !== 'verified' && i.status !== 'skipped');
	}

	async start() {
		if (this.running) return;
		this.running = true;
		const uploaded: string[] = [];
		// the live list: files added or removed during the run count
		for (let it; (it = this.items.find((i) => i.status === 'queued'));) {
			it.status = 'uploading';
			try {
				const r = await uploadBundle(it.file, (p) => (it.pct = p));
				it.status = 'verified';
				it.note = `${r.files} files · ${r.replaced ? 'replaced the old zip' : 'new'} · reloaded in ${r.reload_ms} ms`;
				uploaded.push(r.version);
			} catch (e) {
				it.status = 'error';
				it.note = message(e);
			}
		}
		await status.refresh();
		// status lists bundles newest first, in the config's own version order
		const newest = status.value?.bundles.find((b) => uploaded.includes(b.version))?.version;
		if (this.setDefault && newest) {
			const r = await config
				.patch((c) => (c.defaultBundle = newest))
				.catch((e) => ({ saved: false, errors: [message(e)] }));
			toast(r.saved ? `${newest} is the default now` : (r.errors[0] ?? 'Default not saved'), {
				error: !r.saved
			});
			await status.refresh();
		} else if (uploaded.length)
			toast(`${uploaded.length} bundle${uploaded.length === 1 ? '' : 's'} verified and on disk`);
		this.running = false;
	}
}

export const queue = new UploadQueue();
