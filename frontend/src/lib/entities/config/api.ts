import { create, fromBinary, toBinary } from '@bufbuild/protobuf';
import { request } from '$lib/shared/lib/api';
import { ConfigSchema, type Config } from './gen/edgegateway/config/v1/config_pb';

const url = '/__gateway/config';
const protobuf = 'application/x-protobuf';

export const emptyConfig = () => create(ConfigSchema);

export async function loadConfig(): Promise<Config> {
	const res = await request(url, { headers: { Accept: protobuf } });
	if (!res.ok) throw new Error(`config: ${res.status}`);
	return fromBinary(ConfigSchema, new Uint8Array(await res.arrayBuffer()));
}

export interface SaveResult {
	saved: boolean;
	errors: string[];
	issues: string[];
}

export async function saveConfig(cfg: Config, dryRun = false): Promise<SaveResult> {
	const res = await request(url, {
		method: 'PUT',
		headers: { 'Content-Type': protobuf, ...(dryRun ? { 'X-Dry-Run': '1' } : {}) },
		body: toBinary(ConfigSchema, cfg) as Uint8Array<ArrayBuffer>
	});
	if (res.status !== 200 && res.status !== 422) throw new Error((await res.text()).trim());
	const out = (await res.json()) as SaveResult;
	return { saved: out.saved, errors: out.errors ?? [], issues: out.issues ?? [] };
}
