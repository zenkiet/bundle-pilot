import type { Component } from 'svelte';
import { tick } from 'svelte';
import { goto } from '$app/navigation';
import { resolve } from '$app/paths';
import {
	Cloud,
	FlaskConical,
	LayoutGrid,
	ListChecks,
	Package,
	Shield,
	SlidersHorizontal,
	Star,
	Table2,
	Upload
} from '@lucide/svelte';

export type Path = '/' | '/upload' | '/settings';
export type Tab = 'overview' | 'rules' | 'bundles' | 'general' | 'account' | 'source' | 'default';
export type Section =
	'rules' | 'backend' | 'bundles' | 'tester' | 'general' | 'account' | 'source' | 'default';
type Icon = Component<{ size?: number | string }>;

export const pages: { label: string; icon: Icon; path: Path; tab?: Tab }[] = [
	{ label: 'Overview', icon: LayoutGrid, path: '/', tab: 'overview' },
	{ label: 'Upload bundle', icon: Upload, path: '/upload' },
	{ label: 'Settings', icon: SlidersHorizontal, path: '/settings', tab: 'general' }
];

export const sections: {
	id: Section;
	label: string;
	icon: Icon;
	path: Path;
	tab: Tab;
	badge?: (n: { rules: number; bundles: number }) => string;
}[] = [
	{
		id: 'rules',
		label: 'Rules',
		icon: ListChecks,
		path: '/',
		tab: 'rules',
		badge: (n) => String(n.rules)
	},
	{ id: 'backend', label: 'Backend table', icon: Table2, path: '/', tab: 'rules' },
	{
		id: 'bundles',
		label: 'Bundles',
		icon: Package,
		path: '/',
		tab: 'bundles',
		badge: (n) => String(n.bundles)
	},
	{ id: 'tester', label: 'Decision tester', icon: FlaskConical, path: '/', tab: 'overview' },
	{ id: 'general', label: 'General', icon: SlidersHorizontal, path: '/settings', tab: 'general' },
	{ id: 'account', label: 'Admin account', icon: Shield, path: '/settings', tab: 'account' },
	{ id: 'source', label: 'Bundle source', icon: Cloud, path: '/settings', tab: 'source' },
	{ id: 'default', label: 'Default bundle', icon: Star, path: '/settings', tab: 'default' }
];

class Nav {
	section = $state<Section | null>(null);
	tab = $state<Tab>('overview');
}

export const nav = new Nav();

/** Open a page or one of its sections: switch the phone tab, scroll, focus and flash it. */
export async function jump(section: Section | null = null, path: Path = '/') {
	const sec = sections.find((s) => s.id === section);
	await goto(resolve(sec?.path ?? path));
	nav.section = section;
	nav.tab = sec?.tab ?? pages.find((p) => p.path === path)?.tab ?? nav.tab;
	await tick();
	const el = section && document.getElementById(section);
	if (!el) return document.querySelector('main')?.scrollTo({ top: 0, behavior: 'smooth' });
	el.scrollIntoView({ behavior: 'smooth', block: 'center' });
	el.focus({ preventScroll: true });
	el.classList.remove('flash');
	void el.offsetWidth;
	el.classList.add('flash');
}
