// Package engine models Watchtower e2e cases and streams generators.
//
// A Case is a full configuration vector: every Watchtower flag domain plus
// topology. Product is a streamed cartesian iterator. Random draws independent
// full vectors. File loads named YAML regressions. The scheduler shards,
// resumes, and fans cases out to DinD workers without importing Watchtower.
package engine
