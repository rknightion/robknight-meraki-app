import { PLUGIN_BASE_URL, ROUTES } from '../../constants';

/**
 * URL for the top-level Firmware & Lifecycle scene. Mirrors the shape of
 * the other per-area `urlFor…` helpers so cross-links don't hand-build
 * paths.
 */
export function urlForFirmware(): string {
  return `${PLUGIN_BASE_URL}/${ROUTES.Firmware}`;
}
