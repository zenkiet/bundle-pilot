import type { JsonObject, JsonValue } from '@bufbuild/protobuf';

export interface WhenRow {
	fact: string;
	value: string;
}

// Rows show strings bare and everything else as JSON, so "10%" stays a string
// while 2020210, true and ["a","b"] keep their JSON types.
export function toRows(when?: JsonObject): WhenRow[] {
	return Object.entries(when ?? {}).map(([fact, v]) => ({
		fact,
		value: typeof v === 'string' ? v : JSON.stringify(v)
	}));
}

export function fromRows(rows: WhenRow[]): JsonObject {
	const obj: JsonObject = {};
	for (const { fact, value } of rows) {
		if (fact.trim()) obj[fact.trim()] = parseValue(value.trim());
	}
	return obj;
}

// parseValue keeps JSON types, except a number JSON.parse would rewrite (past 2^53, or 1.10):
// that stays text, so the gateway compares the digits as typed.
function parseValue(text: string): JsonValue {
	if (/^(-?\d+(\.\d+)?|true|false|\[.*\])$/s.test(text)) {
		try {
			const v = JSON.parse(text) as JsonValue;
			if (typeof v !== 'number' || String(v) === text) return v;
		} catch {
			/* keep as text */
		}
	}
	return text;
}
