package canary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// ErrRollback is returned when the controller rolls the canary back.
var ErrRollback = errors.New("canary rolled back")

const maxHolds = 3

type logLine struct {
	Time           string `json:"time"`
	Step           int    `json:"step"`
	Weight         int    `json:"weight"`
	Action         string `json:"action"`
	Reason         string `json:"reason"`
	CanaryRequests int    `json:"canary_requests"`
	CanaryErrors   int    `json:"canary_errors"`
	CanaryP95Ms    int64  `json:"canary_p95_ms"`
	StableRequests int    `json:"stable_requests"`
	StableErrors   int    `json:"stable_errors"`
}

func writeLog(log io.Writer, step, weight int, d Decision, w Window) {
	line := logLine{
		Time:           time.Now().UTC().Format(time.RFC3339Nano),
		Step:           step,
		Weight:         weight,
		Action:         string(d.Action),
		Reason:         d.Reason,
		CanaryRequests: w.Canary.Requests,
		CanaryErrors:   w.Canary.Errors,
		CanaryP95Ms:    w.Canary.P95.Milliseconds(),
		StableRequests: w.Stable.Requests,
		StableErrors:   w.Stable.Errors,
	}
	b, err := json.Marshal(line)
	if err != nil {
		return
	}
	if log != nil {
		_, _ = log.Write(append(b, '\n'))
	}
}

// Run steps the canary weight through steps and evaluates the gate each step.
func Run(ctx context.Context, p *Proxy, steps []int, stepDuration time.Duration, gate Gate, log io.Writer) error {
	for i, step := range steps {
		holds := 0
		for {
			p.SetWeight(step)
			p.TakeWindow()

			select {
			case <-ctx.Done():
				p.SetWeight(0)
				return ctx.Err()
			case <-time.After(stepDuration):
			}

			w := p.TakeWindow()
			d := gate.Evaluate(w)
			writeLog(log, i, step, d, w)

			switch d.Action {
			case Rollback:
				p.SetWeight(0)
				return fmt.Errorf("%w: %s", ErrRollback, d.Reason)
			case Hold:
				holds++
				if holds >= maxHolds {
					p.SetWeight(0)
					return fmt.Errorf("%w: %s", ErrRollback, "not enough canary requests after 3 holds")
				}
				continue
			case Promote:
			}
			break
		}
	}
	return nil
}
