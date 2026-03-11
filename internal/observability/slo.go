package observability

import (
	"encoding/json"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

type SLOTargets struct {
	MinDeliverySuccessRate float64
	MaxRetryRate           float64
	MaxQueueBacklog        uint64
}

type SLOReport struct {
	Timestamp time.Time         `json:"timestamp"`
	Targets   SLOTargets        `json:"targets"`
	SLI       SLIValues         `json:"sli"`
	Breaches  []SLOBreach       `json:"breaches"`
	Status    string            `json:"status"`
	Raw       map[string]uint64 `json:"raw,omitempty"`
}

type SLIValues struct {
	DeliverySuccessRate float64 `json:"delivery_success_rate"`
	RetryRate           float64 `json:"retry_rate"`
	QueueBacklog        uint64  `json:"queue_backlog"`
}

type SLOBreach struct {
	Name     string  `json:"name"`
	Value    float64 `json:"value"`
	Target   float64 `json:"target"`
	Operator string  `json:"operator"`
}

func LoadSLOTargetsFromEnv() SLOTargets {
	return SLOTargets{
		MinDeliverySuccessRate: envFloat("MTA_SLO_MIN_DELIVERY_SUCCESS_RATE", 0.99),
		MaxRetryRate:           envFloat("MTA_SLO_MAX_RETRY_RATE", 0.20),
		MaxQueueBacklog:        envUint64("MTA_SLO_MAX_QUEUE_BACKLOG", 50000),
	}
}

func EvaluateSLO(snapshot map[string]uint64, targets SLOTargets) SLOReport {
	success := snapshot["worker_delivery_success_total"]
	tempFail := snapshot["worker_temporary_failure_total"]
	permFail := snapshot["worker_permanent_bounce_total"]
	totalAttempts := success + tempFail + permFail

	successRate := 1.0
	retryRate := 0.0
	if totalAttempts > 0 {
		successRate = float64(success) / float64(totalAttempts)
		retryRate = float64(tempFail) / float64(totalAttempts)
	}

	queued := snapshot["smtp_queued_messages_total"]
	acked := snapshot["worker_ack_sent_total"]
	failed := snapshot["worker_mark_failed_total"]
	backlog := uint64(0)
	if queued > acked+failed {
		backlog = queued - acked - failed
	}

	report := SLOReport{
		Timestamp: time.Now().UTC(),
		Targets:   targets,
		SLI: SLIValues{
			DeliverySuccessRate: round4(successRate),
			RetryRate:           round4(retryRate),
			QueueBacklog:        backlog,
		},
		Raw:    snapshot,
		Status: "ok",
	}
	if successRate < targets.MinDeliverySuccessRate {
		report.Breaches = append(report.Breaches, SLOBreach{
			Name: "delivery_success_rate", Value: round4(successRate), Target: round4(targets.MinDeliverySuccessRate), Operator: ">=",
		})
	}
	if retryRate > targets.MaxRetryRate {
		report.Breaches = append(report.Breaches, SLOBreach{
			Name: "retry_rate", Value: round4(retryRate), Target: round4(targets.MaxRetryRate), Operator: "<=",
		})
	}
	if backlog > targets.MaxQueueBacklog {
		report.Breaches = append(report.Breaches, SLOBreach{
			Name: "queue_backlog", Value: float64(backlog), Target: float64(targets.MaxQueueBacklog), Operator: "<=",
		})
	}
	if len(report.Breaches) > 0 {
		report.Status = "breach"
	}
	return report
}

func (r SLOReport) JSON() []byte {
	b, _ := json.Marshal(r)
	return b
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}

func envFloat(k string, def float64) float64 {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return n
}

func envUint64(k string, def uint64) uint64 {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}
