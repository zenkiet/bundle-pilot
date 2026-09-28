import { equals } from '@bufbuild/protobuf';
import {
	RuleSchema,
	SourceSchema,
	type Config
} from '$lib/entities/config/gen/edgegateway/config/v1/config_pb';

export interface Change {
	what: string;
	diff: string;
	kind: 'added' | 'changed' | 'removed';
}

export function diff(a: Config, b: Config): Change[] {
	const out: Change[] = [];
	if (a.defaultBundle !== b.defaultBundle)
		out.push({
			what: 'Default bundle',
			diff: `${a.defaultBundle || 'newest'} → ${b.defaultBundle || 'newest'}`,
			kind: 'changed'
		});
	if (a.projectName !== b.projectName || a.environment !== b.environment)
		out.push({
			what: 'Project',
			diff: `${b.projectName || 'Edge gateway'}${b.environment ? ` · ${b.environment}` : ''}`,
			kind: 'changed'
		});
	if (b.auth?.password)
		out.push({ what: 'Admin password', diff: `set for ${b.auth.username}`, kind: 'changed' });
	else if (a.auth?.username !== b.auth?.username)
		out.push({
			what: 'Admin username',
			diff: `${a.auth?.username || 'none'} → ${b.auth?.username || 'none'}`,
			kind: 'changed'
		});
	if (a.dateFormat !== b.dateFormat)
		out.push({
			what: 'Date format',
			diff: `${a.dateFormat || 'MM.dd.YYYY'} → ${b.dateFormat || 'MM.dd.YYYY'}`,
			kind: 'changed'
		});
	const before = new Map(a.rules.map((r, i) => [r.id || `#${i + 1}`, r]));
	b.rules.forEach((r, i) => {
		const key = r.id || `#${i + 1}`;
		const prev = before.get(key);
		before.delete(key);
		if (!prev) out.push({ what: `Rule · ${key}`, diff: `serves ${r.bundle}`, kind: 'added' });
		else if (!equals(RuleSchema, prev, r))
			out.push({
				what: `Rule · ${key}`,
				diff: prev.bundle === r.bundle ? 'conditions changed' : `${prev.bundle} → ${r.bundle}`,
				kind: 'changed'
			});
	});
	for (const [key] of before) out.push({ what: `Rule · ${key}`, diff: 'deleted', kind: 'removed' });
	for (const k of new Set([...Object.keys(a.backend), ...Object.keys(b.backend)])) {
		const from = a.backend[k],
			to = b.backend[k];
		if (from === to) continue;
		out.push({
			what: 'Backend table',
			diff: !from ? `${k} → ${to} added` : !to ? `${k} removed` : `${k}: ${from} → ${to}`,
			kind: !from ? 'added' : !to ? 'removed' : 'changed'
		});
	}
	if (
		!equals(
			SourceSchema,
			a.source ?? ({ $typeName: 'edgegateway.config.v1.Source' } as never),
			b.source ?? ({ $typeName: 'edgegateway.config.v1.Source' } as never)
		)
	)
		out.push({
			what: 'Source',
			diff: `${a.source?.type || 'local'} → ${b.source?.type || 'local'}`,
			kind: 'changed'
		});
	return out;
}
