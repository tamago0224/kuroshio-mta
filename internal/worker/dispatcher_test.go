package worker

import (
	"errors"
	"testing"
	"time"

	"github.com/tamago0224/orinoco-mta/internal/config"
	"github.com/tamago0224/orinoco-mta/internal/delivery"
	"github.com/tamago0224/orinoco-mta/internal/model"
)

func TestBackoff(t *testing.T) {
	schedule := []time.Duration{5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour, 24 * time.Hour}
	cases := map[int]time.Duration{
		0: 5 * time.Minute,
		1: 30 * time.Minute,
		2: 2 * time.Hour,
		3: 6 * time.Hour,
		4: 24 * time.Hour,
		9: 24 * time.Hour,
	}
	for attempts, want := range cases {
		if got := backoff(attempts, schedule); got != want {
			t.Fatalf("attempts=%d got=%s want=%s", attempts, got, want)
		}
	}
}

func TestShouldFailBySMTPClassAndPolicy(t *testing.T) {
	cfg := config.Config{
		MaxAttempts: 12,
		MaxRetryAge: 5 * 24 * time.Hour,
	}
	msg := &model.Message{
		Attempts:  0,
		CreatedAt: time.Now().Add(-time.Hour),
	}
	permanent := []error{&delivery.SMTPResponseError{Code: 550, Line: "550 mailbox unavailable"}}
	temporary := []error{&delivery.SMTPResponseError{Code: 450, Line: "450 temp fail"}}
	mixed := []error{
		&delivery.SMTPResponseError{Code: 550, Line: "550 mailbox unavailable"},
		&delivery.SMTPResponseError{Code: 421, Line: "421 service not available"},
	}
	unknown := []error{errors.New("dial timeout")}

	if !shouldFail(msg, permanent, cfg, time.Now()) {
		t.Fatal("permanent-only SMTP errors should fail")
	}
	if shouldFail(msg, temporary, cfg, time.Now()) {
		t.Fatal("temporary SMTP errors should retry")
	}
	if shouldFail(msg, mixed, cfg, time.Now()) {
		t.Fatal("mixed SMTP errors should retry (temporary takes precedence)")
	}
	if shouldFail(msg, unknown, cfg, time.Now()) {
		t.Fatal("unknown/network errors should retry")
	}
}

func TestShouldFailByAttemptsAndAge(t *testing.T) {
	cfg := config.Config{
		MaxAttempts: 3,
		MaxRetryAge: 5 * 24 * time.Hour,
	}
	now := time.Now()

	attemptedOut := &model.Message{
		Attempts:  3,
		CreatedAt: now.Add(-time.Hour),
	}
	if !shouldFail(attemptedOut, []error{errors.New("any")}, cfg, now) {
		t.Fatal("max attempts reached should fail")
	}

	tooOld := &model.Message{
		Attempts:  0,
		CreatedAt: now.Add(-(5*24*time.Hour + time.Minute)),
	}
	if !shouldFail(tooOld, []error{errors.New("any")}, cfg, now) {
		t.Fatal("max retry age exceeded should fail")
	}
}
