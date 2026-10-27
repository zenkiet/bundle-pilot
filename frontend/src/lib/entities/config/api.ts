import { create, fromBinary, toBinary, toJson } from '@bufbuild/protobuf';
import { request, send } from '#lib/shared/lib/api.ts';
import { ConfigSchema, type Config } from './gen/bundlepilot/config/v1/config_pb';

const url = '/__gateway/config';
const protobuf = 'application/x-protobuf';

export const emptyConfig = () => create(ConfigSchema);

/** Thrown by a save whose config changed on the gateway since it was loaded. */
export const stale = new Error(
	'The config changed elsewhere. Reload to see it; unsaved edits are dropped.'
);

export async function loadConfig(): Promise<{ config: Config; etag: string }> {
	const res = await send(url, { headers: { Accept: protobuf } });
	const config = fromBinary(ConfigSchema, new Uint8Array(await res.arrayBuffer()));
	return { config, etag: res.headers.get('ETag') ?? '' };
}

export interface SaveResult {
	saved: boolean;
	errors: string[];
	issues: string[];
	etag: string;
}

export async function saveConfig(cfg: Config, dryRun = false, etag = ''): Promise<SaveResult> {
	const headers: Record<string, string> = { 'Content-Type': protobuf };
	if (dryRun) headers['X-Dry-Run'] = '1';
	if (etag) headers['If-Match'] = etag;
	const res = await request(url, {
		method: 'PUT',
		headers,
		body: toBinary(ConfigSchema, cfg) as Uint8Array<ArrayBuffer>
	});
	if (res.status === 412) throw stale;
	if (res.status !== 200 && res.status !== 422) throw new Error((await res.text()).trim());
	const out = (await res.json()) as SaveResult;
	return {
		saved: out.saved,
		errors: out.errors ?? [],
		issues: out.issues ?? [],
		etag: res.headers.get('ETag') ?? ''
	};
}

/** What /data would answer, and why each rule before the pick passed. */
export interface Decision {
	bundle: string;
	via: string;
	why: string[];
	error?: string;
}

/** Decides facts, JSON as an app posts them, under cfg as if it were saved. Nothing is recorded. */
export async function decide(facts: string, cfg: Config): Promise<Decision> {
	const res = await send('/__gateway/decide', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		// facts go in as typed, so big ids keep their digits
		body: `{"facts":${facts},"config":${JSON.stringify(toJson(ConfigSchema, cfg))}}`
	});
	return res.json();
}
