package models

import (
    "math"
    "time"
)

// Delay model constants to avoid magic numbers
const (
    // Small value to prevent division by zero or negative denominators
    epsilon = 1e-9
)

// calculateUtilization returns the traffic intensity (ρ) of the queue with
// time-based decay that reduces the impact of long-running jobs.
//
//  ρ(t) = (λ / μ) * decay(t)
//
// decay(t) halves the contribution every 30 minutes.
func calculateUtilization(lambda, mu float64, jobDuration time.Duration) float64 {
    if mu <= 0 {
        return 1 // saturated – no service capacity
    }

    // Time-based exponential decay to gradually "forget" old work.
    const halfLife = 30 * time.Minute
    decay := math.Exp(-math.Ln2 * jobDuration.Seconds() / halfLife.Seconds())

    rho := (lambda / mu) * decay
    if rho >= 1-epsilon {
        return 1 - epsilon
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
// calculateWqMM1 returns the expected waiting time in an M/M/1 queue with
// time-decay adjustment.
func calculateWqMM1(lambda, mu float64, jobDuration time.Duration) float64 {
    rho := calculateUtilization(lambda, mu, jobDuration)

    // Effective arrival rate after decay for stability.
    const halfLife = 30 * time.Minute
    decay := math.Exp(-math.Ln2 * jobDuration.Seconds() / halfLife.Seconds())
    effectiveLambda := lambda * decay

    denom := mu - effectiveLambda
    if denom <= 0 {
        return math.Inf(1)
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
// calculateWqMG1 returns the expected waiting time for an M/G/1 queue with
// time-decay adjustment (Pollaczek-Khinchine formula).
func calculateWqMG1(lambda, mu, varianceS float64, jobDuration time.Duration) float64 {
    rho := calculateUtilization(lambda, mu, jobDuration)
    if rho >= 1-epsilon {
        return math.Inf(1)
    }

    // Apply decay to arrival rate.
    const halfLife = 30 * time.Minute
    decay := math.Exp(-math.Ln2 * jobDuration.Seconds() / halfLife.Seconds())

    meanService := 1.0 / mu
    secondMoment := varianceS + meanService*meanService

    return (lambda * decay * secondMoment) / (2 * (1 - rho))
}
