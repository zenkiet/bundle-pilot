class Media {
	compact = $state(false);

	constructor() {
		const q = matchMedia('(max-width: 767px)');
		this.compact = q.matches;
		q.addEventListener('change', (e) => (this.compact = e.matches));
	}
}

export const media = new Media();
