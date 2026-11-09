package canary

import (
	"fmt"
	"time"
)

// Action is the outcome of a gate evaluation.
type Action string

const (
	// Promote moves the canary to the next step.
	Promote Action = "promote"
	// Hold repeats the current step.
	Hold Action = "hold"
	// Rollback sets the canary weight to zero.
	Rollback Action = "rollback"
)

// Decision is the result of a gate evaluation.
type Decision struct {
	Action Action
	Reason string
}

// Gate holds the thresholds for a canary evaluation.
type Gate struct {
	MaxErrorRate float64
	MaxP95       time.Duration
	MinRequests  int
	Margin       float64
}

func errorRate(s UpstreamStats) float64 {
	if s.Requests == 0 {
		return 0
	}
	return float64(s.Errors) / float64(s.Requests)
}

// Evaluate checks the window against the gate thresholds.
func (g Gate) Evaluate(w Window) Decision {
	if w.Canary.Requests < g.MinRequests {
		return Decision{Action: Hold, Reason: "not enough canary requests"}
	}

	margin := g.Margin
	if margin == 0 {
		margin = 0.02
	}

	canaryRate := errorRate(w.Canary)
	if canaryRate > g.MaxErrorRate {
		return Decision{
			Action: Rollback,
			Reason: fmt.Sprintf("canary error rate %.4f exceeds max %.4f", canaryRate, g.MaxErrorRate),
		}
	}

	stableRate := errorRate(w.Stable)
	if canaryRate > stableRate+margin {
		return Decision{
			Action: Rollback,
			Reason: fmt.Sprintf("canary error rate %.4f exceeds stable %.4f plus margin %.4f", canaryRate, stableRate, margin),
		}
	}

	if g.MaxP95 > 0 && w.Canary.P95 > g.MaxP95 {
		return Decision{
			Action: Rollback,
			Reason: fmt.Sprintf("canary p95 %s exceeds max %s", w.Canary.P95, g.MaxP95),
		}
	}

	return Decision{Action: Promote, Reason: "canary within gate"}
}
