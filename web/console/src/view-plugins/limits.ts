// Kept in the capacity register by TestViewPluginBoundsAreRegistered.
export const PLUGIN_LIMITS = {
  files: 2,
  archiveBytes: 33554432,
  expandedBytes: 33554432,
  manifestBytes: 8192,
  coverSide: 256,
  packages: 8,
  messageBytes: 1048576,
  sessions: 256,
  pending: 16,
  requestMilliseconds: 10000,
  requestsPerSecond: 32,
  textCharacters: 256,
  catalogBytes: 1048576,
  catalogEntries: 256,
  updateMilliseconds: 60000,
} as const
