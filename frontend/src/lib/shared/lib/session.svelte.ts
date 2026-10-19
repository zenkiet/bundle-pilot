const KEY = 'gw.auth';

function read() {
	try {
		return sessionStorage.getItem(KEY);
	} catch {
		return null;
	}
}

class Session {
	header = $state(read());
	setup = $state(''); // first-run token from the gateway log

	set(user: string, password: string) {
		this.header =
			'Basic ' + btoa(String.fromCharCode(...new TextEncoder().encode(`${user}:${password}`)));
		sessionStorage.setItem(KEY, this.header);
	}

	clear() {
		this.header = null;
		sessionStorage.removeItem(KEY);
	}
}

export const session = new Session();
