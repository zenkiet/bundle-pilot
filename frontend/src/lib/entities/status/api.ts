import { getJSON } from '$lib/shared/lib/api';

export interface BundleInfo {
	version: string;
	files: number;
	bytes: number;
	zip_bytes: number;
	mod_time: string;
}

export interface RuleInfo {
	id: string;
	bundle: string;
	active: boolean;
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
	rules: RuleInfo[];
	decisions: Record<string, number>;
	issues: string[];
}

export const loadStatus = () => getJSON<Status>('/__gateway/status');

export interface Decision {
	bundle: string;
	via: string;
}

export async function testDecision(facts: string): Promise<Decision> {
	const res = await fetch('/__gateway/data', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json', 'X-Dry-Run': '1' },
		body: facts
	});
	if (!res.ok) throw new Error((await res.text()).trim());
	return res.json();
}
