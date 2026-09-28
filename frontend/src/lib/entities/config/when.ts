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

function parseValue(text: string): JsonValue {
	if (/^(-?\d+(\.\d+)?|true|false|\[.*\])$/s.test(text)) {
		try {
			return JSON.parse(text) as JsonValue;
		} catch {
			/* keep as text */
		}
	}
	return text;
}
