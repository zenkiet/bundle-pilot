import { clone, equals } from '@bufbuild/protobuf';
import { session } from '#lib/shared/lib/session.svelte.ts';
import { emptyConfig, loadConfig, saveConfig, type SaveResult } from './api.ts';
import { ConfigSchema, type Config } from './gen/bundlepilot/config/v1/config_pb';

class ConfigStore {
	saved = $state<Config>(emptyConfig());
	draft = $state<Config>(emptyConfig());
	errors = $state<string[]>([]);
	issues = $state<string[]>([]);
	busy = $state(false);
	loaded = $state(false);
	dirty = $derived(!equals(ConfigSchema, this.saved, this.draft));
	/** ETag of `saved`: a save sends it, so it cannot overwrite a change made elsewhere. */
	etag = '';

	async load() {
		const { config: c, etag } = await loadConfig();
		this.saved = c;
		this.draft = clone(ConfigSchema, c);
		this.etag = etag;
		this.errors = [];
		this.loaded = true;
	}

	validate(): Promise<SaveResult> {
		return this.apply(saveConfig(this.draft, true));
	}

	async save(): Promise<SaveResult> {
		this.busy = true;
		try {
			const auth = this.draft.auth;
			const r = await this.apply(saveConfig(this.draft, false, this.etag));
			if (r.saved) {
				if (auth?.password) {
					session.set(auth.username, auth.password);
					auth.password = '';
					auth.passwordHash = '***';
				}
				this.saved = clone(ConfigSchema, this.draft);
				this.etag = r.etag;
			}
			return r;
		} finally {
			this.busy = false;
		}
	}

	/** Saves one edit on top of the saved config right away; the rest of the draft stays unsaved. */
	async patch(edit: (c: Config) => void): Promise<SaveResult> {
		const next = clone(ConfigSchema, this.saved);
		edit(next);
		const r = await saveConfig(next, false, this.etag);
		if (r.saved) {
			this.saved = next;
			this.etag = r.etag;
			edit(this.draft);
		}
		return r;
	}

	reset() {
		this.draft = clone(ConfigSchema, this.saved);
		this.errors = [];
		this.issues = [];
	}

	private async apply(p: Promise<SaveResult>) {
		const r = await p;
		this.errors = r.errors;
		this.issues = r.issues;
		return r;
	}
}

export const config = new ConfigStore();
