// Kept in the capacity register by TestViewPluginBoundsAreRegistered.
export const PLUGIN_LIMITS = {
  files: 2,
  archiveBytes: 262144,
  expandedBytes: 524288,
  manifestBytes: 8192,
  coverSide: 256,
  packages: 8,
  messageBytes: 1048576,
  sessions: 256,
  pending: 16,
  requestMilliseconds: 10000,
  requestsPerSecond: 32,
  textCharacters: 256,
} as const
