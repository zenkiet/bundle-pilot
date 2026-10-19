import { fromBinary } from '@bufbuild/protobuf';
import { getJSON, request } from '#lib/shared/lib/api.ts';
import { ConfigSchema, type Config } from './gen/bundlepilot/config/v1/config_pb';

/** One saved config: when, and by whom (an admin, token:<name>, setup or cli). */
export interface Revision {
	id: string;
	at: string;
	by: string;
}

export const listHistory = () => getJSON<Revision[]>('/__gateway/history');

/** A saved config as clients see it: secrets masked, so a save keeps the live ones. */
export async function loadRevision(id: string): Promise<Config> {
	const res = await request(`/__gateway/history/${encodeURIComponent(id)}`, {
		headers: { Accept: 'application/x-protobuf' }
	});
	if (!res.ok) throw new Error(`revision ${id}: ${res.status}`);
	return fromBinary(ConfigSchema, new Uint8Array(await res.arrayBuffer()));
}
