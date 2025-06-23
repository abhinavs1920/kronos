package models

// This file exposes public wrappers around the internal (unexported) queueing
// helpers so that other packages, such as the EnergyAware scheduler plugin,
// can consume them without re-implementing the mathematics. Keeping the core
// formulas unexported in `queue.go` makes it easier to unit-test the public
// surface while protecting the internal helpers from accidental misuse.

// CalculateUtilization returns the system utilisation ρ = λ / μ.
func CalculateUtilization(lambda, mu float64) float64 {
    return calculateUtilization(lambda, mu)
}

// CalculateWqMM1 returns the mean waiting time in queue for an M/M/1 system.
func CalculateWqMM1(lambda, mu float64) float64 {
    return calculateWqMM1(lambda, mu)
}

// CalculateWqMG1 returns the mean waiting time in queue for an M/G/1 system.
func CalculateWqMG1(lambda, mu, varianceS float64) float64 {
    return calculateWqMG1(lambda, mu, varianceS)
}
