package app

import "runtime"

// hashWorkers picks the identity-hash concurrency for a source: all CPUs for
// local volumes, the configured low cap for network shares.
func (a *App) hashWorkers(network bool) int {
	if network {
		return a.Config().Behaviour.NetworkHashWorkers
	}
	return runtime.NumCPU()
}
