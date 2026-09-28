export function clickOutside(node: HTMLElement, onOutside: () => void) {
	const handler = (e: PointerEvent) => {
		if (!node.contains(e.target as Node)) onOutside();
	};
	document.addEventListener('pointerdown', handler, true);
	return { destroy: () => document.removeEventListener('pointerdown', handler, true) };
}
