package models

import "math"

// Delay model constants to avoid magic numbers
const (
    // Small value to prevent division by zero or negative denominators
    epsilon = 1e-9
)

// calculateUtilization returns the traffic intensity (ρ) of the queue.
//
//  ρ = λ / μ
//
// Where:
//  λ (lambda) is the arrival rate – average number of jobs arriving per unit time.
//  μ (mu)     is the service rate  – average number of jobs the server can complete per unit time.
//
// The utilisation must satisfy 0 ≤ ρ < 1 for the steady-state formulas that follow to be valid. In
// practice we cap the result to the open interval (0,1) to avoid division-by-zero behaviour when the
// caller accidentally passes λ ≥ μ.
func calculateUtilization(lambda, mu float64) float64 {
    if mu <= 0 {
        return 1 // Server with zero capacity – system is saturated
    }
    rho := lambda / mu
    if rho >= 1-epsilon {
        return 1 - epsilon // cap just below 1 to keep subsequent formulas finite
    }
    if rho < 0 {
        return 0
    }
    return rho
}

// calculateWqMM1 returns the expected waiting time in the queue (Wq) for an M/M/1 system.
//
// For a memoryless single-server queue the Pollaczek-Khinchine simplification gives:
//
//   Wq = ρ / (μ - λ)
//       = λ / (μ * (μ - λ))
//
// As ρ → 1 the waiting time grows unbounded, capturing the steep latency increase at high load.
func calculateWqMM1(lambda, mu float64) float64 {
    rho := calculateUtilization(lambda, mu)
    denom := mu - lambda
    if denom <= 0 {
        return math.Inf(1) // Unstable queue – infinite expected waiting time
    }
    return rho / denom
}

// calculateWqMG1 returns the expected waiting time in the queue (Wq) for an M/G/1 system using the
// Pollaczek-Khinchine formula.
//
//   Wq = (λ * E[S^2]) / (2 * (1 - ρ))
//
// where:
//   E[S]   = 1 / μ                  (mean service time)
//   Var(S) = varianceS              (passed by caller)
//   E[S^2] = Var(S) + (E[S])^2
//
// The caller is responsible for supplying an estimated variance of the service time distribution.
// For an exponential service time Var(S) = 1/μ², which makes this formula fall back to the M/M/1
// result.
func calculateWqMG1(lambda, mu, varianceS float64) float64 {
    rho := calculateUtilization(lambda, mu)
    if rho >= 1-epsilon {
        return math.Inf(1) // Unstable system
    }

    if mu <= 0 {
        return math.Inf(1)
    }

    meanS := 1 / mu
    es2 := varianceS + meanS*meanS

    return (lambda * es2) / (2 * (1 - rho))
}
