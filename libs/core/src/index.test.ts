import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createPilot } from './index.ts';

const S1 = { storeID: 'S1', backend: '07.30.2026' };
const KEY = JSON.stringify({ backend: '07.30.2026', storeID: 'S1' });
const settle = () => new Promise((r) => setTimeout(r, 20));

// page fakes one page load: the meta tag, URL, cookie jar, localStorage and the gateway's answer.
function page({ url = 'https://app.test/dashboard', version = '4.100.0', answer = '4.100.0' as string | Error, cookies = true, stored = {} as Record<string, string> } = {}) {
	const jar = new Map<string, string>();
	const seen = { posts: 0, replaced: [] as string[], url };
	const g = globalThis as any;
	g.document = {
		baseURI: 'https://app.test/',
		querySelector: () => (version ? { getAttribute: () => version } : null),
		get cookie() {
			return cookies ? [...jar].map(([k, v]) => `${k}=${v}`).join('; ') : '';
		},
		set cookie(s: string) {
			const [kv] = s.split(';');
			if (cookies) jar.set(kv.slice(0, kv.indexOf('=')), kv.slice(kv.indexOf('=') + 1));
		}
	};
	g.location = { href: url, protocol: 'https:', replace: (u: string) => seen.replaced.push(u) };
	g.history = { state: null, replaceState: (_: unknown, __: string, u: string) => (seen.url = g.location.href = u) };
	Object.defineProperty(g, 'localStorage', { configurable: true, value: { getItem: (k: string) => stored[k] ?? null, setItem: (k: string, v: string) => (stored[k] = v) } });
	g.requestAnimationFrame = (f: () => void) => setTimeout(f, 0);
	g.fetch = async () => {
		seen.posts++;
		if (answer instanceof Error) throw answer;
		return { ok: true, json: async () => ({ bundle: answer, via: 'rule:s1' }) };
	};
	return { seen, jar, stored };
}

test('stays without the gateway, a pin or facts', () => {
	for (const p of [{ version: '' }, { url: 'https://app.test/?bundle=4.105.0' }]) {
		const { seen } = page(p);
		assert.equal(createPilot().check(S1), true);
		assert.equal(seen.posts, 0);
	}
	const { seen, jar } = page();
	const pilot = createPilot();
	for (const f of [null, {}, { storeID: '', backend: null }]) assert.equal(pilot.check(f), true);
	assert.equal(seen.posts, 0);
	assert.equal(jar.size, 0, 'no facts, no cookie');
});

test('first visit asks once, remembers, then answers synchronously', async () => {
	const { seen, jar, stored } = page();
	const pilot = createPilot();
	const first = pilot.check(S1);
	assert.ok(first instanceof Promise);
	assert.equal(decodeURIComponent(jar.get('bundle_facts')!), KEY, 'cookie written before the answer');
	assert.equal(await first, true);
	assert.equal(pilot.check(S1), true);
	assert.equal(seen.posts, 1);
	assert.deepEqual(JSON.parse(stored['bundle-pilot']), { key: KEY, bundle: '4.100.0' });
});

test('a known decision is synchronous, revalidated once per page', async () => {
	const { seen } = page({ stored: { 'bundle-pilot': JSON.stringify({ key: KEY, bundle: '4.100.0' }) } });
	const pilot = createPilot();
	for (let i = 0; i < 4; i++) assert.equal(pilot.check(S1), true);
	await settle();
	assert.equal(seen.posts, 1);
});

test('another bundle: splash, then lands on the url with the marker', async () => {
	const { seen } = page({ answer: '4.105.0' });
	let splash = 0;
	const pilot = createPilot({ beforeSwitch: () => splash++ });
	assert.equal(await pilot.check(S1, '/dashboard/x?q=1#f'), false);
	assert.equal(splash, 1);
	assert.deepEqual(seen.replaced, ['https://app.test/dashboard/x?q=1&ngsw-bypass=4.105.0#f']);
	assert.equal(pilot.check(S1), false, 'no navigation while leaving');
});

test('a background answer that differs switches at the next navigation, without asking again', async () => {
	const { seen } = page({ answer: '4.105.0', stored: { 'bundle-pilot': JSON.stringify({ key: KEY, bundle: '4.100.0' }) } });
	const pilot = createPilot();
	assert.equal(pilot.check(S1), true);
	await settle();
	assert.equal(await pilot.check(S1, '/next'), false);
	assert.deepEqual(seen.replaced, ['https://app.test/next?ngsw-bypass=4.105.0']);
	assert.equal(seen.posts, 1);
});

test('fails open, and a failure is not retried on that page', async () => {
	const { seen } = page({ answer: new Error('down') });
	const pilot = createPilot();
	assert.equal(await pilot.check(S1), true);
	assert.equal(await pilot.check(S1), true);
	assert.equal(seen.posts, 1);
});

test('landing on the asked bundle strips the marker and does not ask again', () => {
	const { seen } = page({ url: 'https://app.test/next?a=1&ngsw-bypass=4.105.0', version: '4.105.0', stored: { 'bundle-pilot': JSON.stringify({ key: KEY, bundle: '4.105.0' }) } });
	const pilot = createPilot();
	assert.equal(seen.url, 'https://app.test/next?a=1');
	assert.equal(pilot.check(S1), true);
	assert.equal(seen.posts, 0);
});

test('landing elsewhere means the gateway disagreed: stay for this page', () => {
	const { seen } = page({ url: 'https://app.test/next?ngsw-bypass=4.105.0', answer: '4.105.0' });
	const warn = console.warn;
	console.warn = () => {};
	const pilot = createPilot();
	console.warn = warn;
	assert.equal(seen.url, 'https://app.test/next');
	assert.equal(pilot.check(S1), true);
	assert.equal(seen.posts, 0);
});

test('never switches for a dead navigation, blocked cookies or stale facts', async () => {
	let { seen } = page({ answer: '4.105.0' });
	assert.equal(await createPilot().check(S1, '/', () => false), true);
	assert.equal(seen.replaced.length, 0);

	({ seen } = page({ answer: '4.105.0', cookies: false }));
	assert.equal(await createPilot().check(S1), true);
	assert.equal(seen.replaced.length, 0);

	({ seen } = page({ answer: '4.105.0' }));
	const pilot = createPilot();
	const old = pilot.check(S1, '/old');
	const now = pilot.check({ storeID: 'S2' }, '/new');
	assert.equal(await old, true, 'the cookie now holds the newer facts');
	assert.equal(await now, false);
	assert.deepEqual(seen.replaced, ['https://app.test/new?ngsw-bypass=4.105.0']);
});

test('a throwing beforeSwitch still switches', async () => {
	const { seen } = page({ answer: '4.105.0' });
	const pilot = createPilot({
		beforeSwitch: () => {
			throw new Error('splash');
		}
	});
	assert.equal(await pilot.check(S1), false);
	assert.equal(seen.replaced.length, 1);
});

test('no document: a no-op', () => {
	delete (globalThis as any).document;
	assert.equal(createPilot().version, '');
	assert.equal(createPilot().check(S1), true);
});
