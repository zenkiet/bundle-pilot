import { message } from '#lib/shared/lib/api.ts';
import { loadStatus, type Status } from './api.ts';

class StatusStore {
	value = $state<Status | null>(null);
	error = $state('');

	async refresh() {
		try {
			this.value = await loadStatus();
			this.error = '';
		} catch (e) {
			this.error = message(e);
		}
	}
}

export const status = new StatusStore();
