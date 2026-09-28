import { clone, equals } from '@bufbuild/protobuf';
import { session } from '$lib/shared/lib/session.svelte';
import { emptyConfig, loadConfig, saveConfig, type SaveResult } from './api';
import { ConfigSchema, type Config } from './gen/edgegateway/config/v1/config_pb';

class ConfigStore {
	saved = $state<Config>(emptyConfig());
	draft = $state<Config>(emptyConfig());
	errors = $state<string[]>([]);
	issues = $state<string[]>([]);
	busy = $state(false);
	loaded = $state(false);
	dirty = $derived(!equals(ConfigSchema, this.saved, this.draft));

	async load() {
		const c = await loadConfig();
		this.saved = c;
		this.draft = clone(ConfigSchema, c);
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
			const r = await this.apply(saveConfig(this.draft));
			if (r.saved) {
				if (auth?.password) {
					session.set(auth.username, auth.password);
					auth.password = '';
					auth.passwordHash = '***';
				}
				this.saved = clone(ConfigSchema, this.draft);
			}
			return r;
		} finally {
			this.busy = false;
		}
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
