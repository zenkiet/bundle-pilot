import assert from 'node:assert/strict';
import { test } from 'node:test';
import { entries, verifyManifest } from './manifest.ts';

const enc = (s: string) => new TextEncoder().encode(s);
const h = (c: string) => c.repeat(64);
const BASE = 'https://ota.test/__gateway/bundles/4.110.0';

test('a manifest verifies only with its own signature, raw or hex', async () => {
	const keys = (await crypto.subtle.generateKey({ name: 'Ed25519' }, true, ['sign', 'verify'])) as CryptoKeyPair;
	const pub = new Uint8Array(await crypto.subtle.exportKey('raw', keys.publicKey));
	const text = enc(`${h('a')} *./index.html\n`);
	const sig = new Uint8Array(await crypto.subtle.sign({ name: 'Ed25519' }, keys.privateKey, text));
	const hex = enc([...sig].map((b) => b.toString(16).padStart(2, '0')).join('') + '\n');

	assert.equal(await verifyManifest(pub, text, sig), true);
	assert.equal(await verifyManifest(pub, text, hex), true);
	assert.equal(await verifyManifest(pub, enc(`${h('b')} *./index.html\n`), sig), false);
});

test('entries map files to download urls, drop precompressed twins and refuse escapes', () => {
	const list = entries(enc(`${h('a')} *./index.html\n${h('b')}  main.js\n${h('c')} *main.js.br\n${h('d')} *a b/x.css.gz\n`), BASE);
	assert.deepEqual(list, [
		{ file_name: 'index.html', file_hash: h('a'), download_url: `${BASE}/index.html` },
		{ file_name: 'main.js', file_hash: h('b'), download_url: `${BASE}/main.js` },
		{ file_name: 'a b/x.css.gz', file_hash: h('d'), download_url: `${BASE}/a%20b/x.css.gz` }
	]);
	assert.equal(entries(enc(`${h('a')} *./index.html\n${h('b')} *../etc/passwd\n`), BASE), undefined);
	assert.equal(entries(enc(`${h('b')} *main.js\n`), BASE), undefined); // no index.html
	assert.equal(entries(enc(`nothex *./index.html\n`), BASE), undefined);
});
