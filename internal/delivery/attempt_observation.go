package delivery

import "context"

type attemptObservationKey struct{}

// AttemptObservation contains transport facts observed during one delivery call.
type AttemptObservation struct {
	TLSApplied bool
}

// WithAttemptObservation attaches a caller-owned observation destination.
func WithAttemptObservation(ctx context.Context, observation *AttemptObservation) context.Context {
	return context.WithValue(ctx, attemptObservationKey{}, observation)
}

func observeTLS(ctx context.Context, applied bool) {
	if observation, ok := ctx.Value(attemptObservationKey{}).(*AttemptObservation); ok && observation != nil {
		observation.TLSApplied = applied
	}
}
