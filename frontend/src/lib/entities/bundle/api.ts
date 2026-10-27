interface UploadResult {
	version: string;
	files: number;
	replaced: boolean;
	reload_ms: number;
}

import { request, root, send } from '#lib/shared/lib/api.ts';
import { session } from '#lib/shared/lib/session.svelte.ts';

export function uploadBundle(file: File, onProgress: (pct: number) => void) {
	return new Promise<UploadResult>((resolve, reject) => {
		const xhr = new XMLHttpRequest();
		xhr.open('PUT', root(`/__gateway/bundles/${encodeURIComponent(file.name)}`));
		if (session.header) xhr.setRequestHeader('Authorization', session.header);
		xhr.upload.onprogress = (e) =>
			e.lengthComputable && onProgress(Math.round((e.loaded / e.total) * 100));
		xhr.onload = () =>
			xhr.status === 200
				? resolve(JSON.parse(xhr.responseText))
				: reject(new Error(xhr.responseText.trim() || `upload failed: ${xhr.status}`));
		xhr.onerror = () => reject(new Error('network error'));
		xhr.send(file);
	});
}

export async function deleteBundle(version: string) {
	await send(`/__gateway/bundles/${encodeURIComponent(version)}.zip`, { method: 'DELETE' });
}

/** Blocks a bundle at once, or lifts the block: blocked bundles stay on disk but are never served. */
export async function setBlocked(version: string, blocked: boolean) {
	await send(`/__gateway/blocked/${encodeURIComponent(version)}`, {
		method: blocked ? 'PUT' : 'DELETE'
	});
}

export async function reloadGateway() {
	const res = await request('/__gateway/reload', { method: 'POST' });
	const out = (await res.json()) as { reload_ms: number; error?: string };
	if (out.error) throw new Error(out.error);
	return out;
}
