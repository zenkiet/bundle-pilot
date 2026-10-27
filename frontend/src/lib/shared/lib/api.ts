import { goto } from '$app/navigation';
import { resolve } from '$app/paths';
import { session } from './session.svelte';

export const unauthorized = new Error('sign in required');

export const root = (path: string) => resolve('/').replace(/\/__gateway\/ui\/$/, '') + path;

export async function request(url: string, init: RequestInit = {}): Promise<Response> {
	const headers = new Headers(init.headers);
	if (session.header) headers.set('Authorization', session.header);
	if (session.setup) headers.set('X-Setup-Token', session.setup);
	const res = await fetch(root(url), { cache: 'no-store', ...init, headers });
	if (res.status === 401) {
		session.clear();
		if (!location.pathname.includes('/login')) void goto(resolve('/login'));
		throw unauthorized;
	}
	return res;
}

/** request, throwing the gateway's own message on an error status. */
export async function send(url: string, init?: RequestInit): Promise<Response> {
	const res = await request(url, init);
	if (!res.ok) throw new Error((await res.text()).trim() || `${url}: ${res.status}`);
	return res;
}

export const getJSON = <T>(url: string): Promise<T> => send(url).then((res) => res.json());

export const mb = (bytes: number) => `${(bytes / 1048576).toFixed(1)} MB`;

export function ago(iso: string): string {
	const s = Math.max(0, (Date.now() - Date.parse(iso)) / 1000);
	if (s < 60) return `${Math.round(s)} s ago`;
	if (s < 3600) return `${Math.round(s / 60)} min ago`;
	if (s < 86400) return `${Math.round(s / 3600)} h ago`;
	return `${Math.round(s / 86400)} d ago`;
}

export const message = (e: unknown) => (e instanceof Error ? e.message : String(e));
