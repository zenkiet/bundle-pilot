/** What CapacitorUpdater.download({ manifest }) takes; the plugin checks each file's SHA-256. */
export interface ManifestEntry {
	file_name: string;
	file_hash: string;
	download_url: string;
}

// ponytail: WebCrypto Ed25519 needs WebView 137+ / iOS 17+; older devices fail closed. Add @noble/ed25519 if they matter.
/** Checks bundle.sha256 against its Ed25519 signature, hex or raw as the gateway accepts it. */
export async function verifyManifest(key: Uint8Array, text: Uint8Array, sig: Uint8Array): Promise<boolean> {
	const hex = new TextDecoder().decode(sig).trim();
	const raw = /^[0-9a-f]{128}$/i.test(hex) ? Uint8Array.from(hex.match(/../g)!, (h) => parseInt(h, 16)) : sig;
	const k = await crypto.subtle.importKey('raw', key as BufferSource, { name: 'Ed25519' }, false, ['verify']);
	return crypto.subtle.verify({ name: 'Ed25519' }, k, raw as BufferSource, text as BufferSource);
}

/**
 * Turns a signed bundle.sha256 (`<hash> *./path` lines) into Capgo entries, or undefined when a line is
 * malformed or index.html is missing. Precompressed `x.br`/`x.gz` copies of a listed `x` are dropped:
 * the gateway does not serve them as files, and Capgo would see duplicate targets.
 */
export function entries(text: Uint8Array, base: string): ManifestEntry[] | undefined {
	const files = new Map<string, string>();
	for (const line of new TextDecoder().decode(text).split('\n').map((l) => l.trim())) {
		if (!line) continue;
		const i = line.indexOf(' ');
		if (i < 0) return undefined;
		const hash = line.slice(0, i).toLowerCase();
		const name = line
			.slice(i + 1)
			.replace(/^[ *]+/, '')
			.replace(/^\.\//, '');
		if (!/^[0-9a-f]{64}$/.test(hash) || name.split('/').some((p) => !p || p === '.' || p === '..')) return undefined;
		files.set(name, hash);
	}
	if (!files.has('index.html')) return undefined;
	return [...files]
		.filter(([name]) => !/\.(br|gz)$/.test(name) || !files.has(name.slice(0, -3)))
		.map(([name, hash]) => ({
			file_name: name,
			file_hash: hash,
			download_url: `${base}/${name.split('/').map(encodeURIComponent).join('/')}`
		}));
}
