package projectassistant

// Empty assets still consume filesystem entries. The byte ceiling alone does
// not bound that cost. All selected folders share this per-execution ceiling.
const MaxSkillAssetFiles = 256

// Connector children have their own durable process reservations; uncertain
// shutdowns retain their reservation across source restarts.
const MaxConnectorProcesses = 4
