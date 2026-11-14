/**
 * Bots stays out of public releases until it is ready: its Settings switch is
 * shown on a dev build (v<x.y.z>-dev.<commit>), or wherever Bots is already on
 * so that it can be turned off. A release without the switch never starts Bots,
 * because the feature is off by default.
 */
export function botsSwitchShown(version: string, enabled: boolean): boolean {
  return enabled || /^v\d+\.\d+\.\d+-dev\.[0-9a-f]+$/.test(version.trim())
}
