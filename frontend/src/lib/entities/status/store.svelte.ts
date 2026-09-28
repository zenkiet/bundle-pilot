import { loadStatus, type Status } from './api';

class StatusStore {
	value = $state<Status | null>(null);
	error = $state('');

	async refresh() {
		try {
			this.value = await loadStatus();
			this.error = '';
		} catch (e) {
			this.error = e instanceof Error ? e.message : String(e);
		}
	}
}

export const status = new StatusStore();
