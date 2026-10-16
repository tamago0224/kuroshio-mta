package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/router"
	"github.com/tamago0224/kuroshio-mta/internal/shaping"
)

func TestMXShapingWaitPreservesEarlierActualFailure(t *testing.T) {
	for _, tc := range []struct {
		name       string
		firstFails bool
	}{{"pure shaping wait", false}, {"earlier actual failure", true}} {
		t.Run(tc.name, func(t *testing.T) {
			client := NewClient(config.Config{})
			client.dane = nil
			client.mtaSTS = nil
			client.resolveMXFn = func(string, time.Duration) ([]router.MXHost, error) {
				return []router.MXHost{{Host: "a.test"}, {Host: "b.test"}}, nil
			}
			calls := 0
			actual := errors.New("dial failed")
			client.deliverHostFn = func(context.Context, string, int, *model.Message, string, bool, *DANEResult) error {
				calls++
				if tc.firstFails && calls == 1 {
					return actual
				}
				return &shaping.DeferredError{Delay: time.Minute, Reason: "limit"}
			}
			err := client.deliverByMX(context.Background(), &model.Message{}, "user@example.test")
			if tc.firstFails {
				if !errors.Is(err, actual) || calls != 2 {
					t.Fatalf("actual failure hidden: %v", err)
				}
			} else {
				var deferred *shaping.DeferredError
				if !errors.As(err, &deferred) || calls != 1 {
					t.Fatalf("shaping wait not preserved: %v", err)
				}
			}
		})
	}
}
