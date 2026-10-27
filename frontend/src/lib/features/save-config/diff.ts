import { create, equals } from '@bufbuild/protobuf';
import {
	RuleSchema,
	SourceSchema,
	type Config,
	type Rule
} from '#lib/entities/config/gen/bundlepilot/config/v1/config_pb.ts';

interface Change {
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
			diff: `${b.projectName || 'Bundle Pilot'}${b.environment ? ` · ${b.environment}` : ''}`,
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
	const name = (r: Rule, i: number) => r.id || `#${i + 1}`;
	const before = new Map(a.rules.map((r, i) => [name(r, i), r]));
	b.rules.forEach((r, i) => {
		const key = name(r, i);
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
	// The first match wins, so moving a rule changes what is served.
	const kept = (x: Config, y: Config) => {
		const other = new Set(y.rules.map(name));
		return x.rules.map(name).filter((k) => other.has(k));
	};
	const order = kept(b, a);
	if (order.join('\n') !== kept(a, b).join('\n'))
		out.push({ what: 'Rule order', diff: order.join(', '), kind: 'changed' });
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
	const local = create(SourceSchema);
	if (!equals(SourceSchema, a.source ?? local, b.source ?? local))
		out.push({
			what: 'Source',
			diff: `${a.source?.type || 'local'} → ${b.source?.type || 'local'}`,
			kind: 'changed'
		});
	return out;
}
