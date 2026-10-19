import { getJSON, request } from '#lib/shared/lib/api.ts';

export type Scope = 'read' | 'upload' | 'admin';

export interface Token {
	name: string;
	scope: Scope;
	created?: string;
}

export const listTokens = () => getJSON<Token[]>('/__gateway/tokens');

/** Makes a token and returns its secret, which the gateway shows this once. */
export async function createToken(name: string, scope: Scope): Promise<string> {
	const res = await request('/__gateway/tokens', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ name, scope })
	});
	if (!res.ok) throw new Error((await res.text()).trim());
	return ((await res.json()) as { token: string }).token;
}

export async function revokeToken(name: string) {
	const res = await request(`/__gateway/tokens/${encodeURIComponent(name)}`, { method: 'DELETE' });
	if (!res.ok) throw new Error((await res.text()).trim());
}
