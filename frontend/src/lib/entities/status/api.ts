import { getJSON } from '#lib/shared/lib/api.ts';

export interface BundleInfo {
	version: string;
	files: number;
	bytes: number;
	zip_bytes: number;
	mod_time: string;
	/** App versions it runs on, from its <meta name="bundle-pilot:native">. */
	native?: string;
	/** Listed but never served: see PUT /__gateway/blocked. */
	blocked?: boolean;
}

export interface Status {
	version: string;
	started_at: string;
	project_name: string;
	environment: string;
	setup_required: boolean;
	auth: boolean;
	default: string;
	default_from: string;
	base_path: string;
	source: string;
	sync?: { at: string; objects: number; error: string };
	signing: boolean;
	loaded_at: string;
	reloads: number;
	last_error: string;
	errors: string[];
	resident_bytes: number;
	bundles: BundleInfo[];
	decisions: Record<string, number>;
	decisions_by_bundle: Record<string, number>;
	issues: string[];
}

export const loadStatus = () => getJSON<Status>('/__gateway/status');
