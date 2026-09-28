import { goto } from '$app/navigation';
import { resolve } from '$app/paths';
import { session } from './session.svelte';

export const unauthorized = new Error('sign in required');

export async function request(url: string, init: RequestInit = {}): Promise<Response> {
	const headers = new Headers(init.headers);
	if (session.header) headers.set('Authorization', session.header);
	const res = await fetch(url, { cache: 'no-store', ...init, headers });
	if (res.status === 401) {
		session.clear();
		if (!location.pathname.includes('/login')) void goto(resolve('/login'));
		throw unauthorized;
	}
	return res;
}

export async function getJSON<T>(url: string): Promise<T> {
	const res = await request(url);
	if (!res.ok) throw new Error(`${url}: ${res.status}`);
	return res.json();
}

export const mb = (bytes: number) => `${(bytes / 1048576).toFixed(1)} MB`;

export function ago(iso: string): string {
	const s = Math.max(0, (Date.now() - Date.parse(iso)) / 1000);
	if (s < 60) return `${Math.round(s)} s ago`;
	if (s < 3600) return `${Math.round(s / 60)} min ago`;
	if (s < 86400) return `${Math.round(s / 3600)} h ago`;
	return `${Math.round(s / 86400)} d ago`;
}

export const message = (e: unknown) => (e instanceof Error ? e.message : String(e));
