export interface UploadResult {
	version: string;
	files: number;
	replaced: boolean;
	reload_ms: number;
}

import { request } from '$lib/shared/lib/api';
import { session } from '$lib/shared/lib/session.svelte';

export function uploadBundle(file: File, onProgress: (pct: number) => void) {
	return new Promise<UploadResult>((resolve, reject) => {
		const xhr = new XMLHttpRequest();
		xhr.open('PUT', `/__gateway/bundles/${encodeURIComponent(file.name)}`);
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
	const res = await request(`/__gateway/bundles/${encodeURIComponent(version)}.zip`, {
		method: 'DELETE'
	});
	if (!res.ok) throw new Error((await res.text()).trim());
}

export async function reloadGateway() {
	const res = await request('/__gateway/reload', { method: 'POST' });
	const out = (await res.json()) as { reload_ms: number; error?: string };
	if (out.error) throw new Error(out.error);
	return out;
}
