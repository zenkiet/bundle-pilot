import { isPlatformBrowser, Location } from '@angular/common';
import {
	ENVIRONMENT_INITIALIZER,
	EnvironmentInjector,
	inject,
	makeEnvironmentProviders,
	PLATFORM_ID,
	runInInjectionContext,
	type EnvironmentProviders
} from '@angular/core';
import { Router, type CanActivateFn, type Route } from '@angular/router';
import { createPilot, type Facts, type PilotOptions } from '@bundle-pilot/core';

export { bundleVersion, type Facts } from '@bundle-pilot/core';

export interface BundlePilotOptions extends PilotOptions {
	/** What the gateway decides on; null while unknown (signed out). Runs in the guard, so inject() works. */
	facts: () => Facts | null | undefined;
}

/**
 * Guards every route, lazy children included: a navigation whose facts call for another bundle
 * becomes a page load of that bundle. Root providers only; a later router.resetConfig drops it.
 */
export function provideBundlePilot({ facts, beforeSwitch, ...options }: BundlePilotOptions): EnvironmentProviders {
	return makeEnvironmentProviders([
		{
			provide: ENVIRONMENT_INITIALIZER,
			multi: true,
			useValue: () => {
				if (!isPlatformBrowser(inject(PLATFORM_ID))) return;
				const env = inject(EnvironmentInjector);
				const router = inject(Router);
				const pilot = createPilot({ ...options, beforeSwitch: beforeSwitch && (() => runInInjectionContext(env, beforeSwitch)) });
				const guard: CanActivateFn = (_, state) => {
					const id = router.getCurrentNavigation()?.id;
					const alive = () => router.getCurrentNavigation()?.id === id; // not cancelled, redirected or superseded
					return pilot.check(facts(), inject(Location).prepareExternalUrl(state.url), alive);
				};
				const guarded = (r: Route): Route =>
					r.redirectTo === undefined // Angular rejects guards on redirects
						? { ...r, canActivate: [...(r.canActivate ?? []), guard], canActivateChild: [...(r.canActivateChild ?? []), guard] }
						: r;
				router.resetConfig(router.config.map(guarded));
			}
		}
	]);
}
