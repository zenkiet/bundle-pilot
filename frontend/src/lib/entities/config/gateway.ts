// How the gateway reads config values, mirrored for display.

/** When a rule's until ends: a bare day runs through its end in UTC, as on the gateway. */
export function untilEnd(until: string): number {
	return Date.parse(until) + (/^\d{4}-\d{2}-\d{2}$/.test(until) ? 864e5 : 0);
}

/**
 * Orders backend keys the way the gateway resolves them: dates in the config's layout by day,
 * anything else as dotted numbers.
 * ponytail: pre-release tags order as text, not as semver.
 */
export function compareKeys(layout: string, a: string, b: string): number {
	const da = day(layout, a);
	const db = day(layout, b);
	return da && db ? da - db : a.localeCompare(b, undefined, { numeric: true });
}

// day reads s as yyyymmdd in a layout such as MM.dd.YYYY, or 0 when it is not a date in it.
function day(layout: string, s: string): number {
	const keys = (layout || 'MM.dd.YYYY').split(/[./-]/);
	const parts = s.split(/[./-]/);
	if (keys.length !== 3 || parts.length !== 3 || !parts.every((p) => /^\d+$/.test(p))) return 0;
	const part = (c: string) => parts[keys.findIndex((k) => k[0].toLowerCase() === c)] ?? '';
	const [y, m, d] = [part('y'), Number(part('m')), Number(part('d'))];
	return y.length === 4 && m >= 1 && m <= 12 && d >= 1 && d <= 31 ? +y * 1e4 + m * 100 + d : 0;
}
