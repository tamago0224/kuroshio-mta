package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"math"
	"net"
	"net/textproto"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type summary struct {
	Address      string  `json:"address"`
	Concurrency  int     `json:"concurrency"`
	Requested    int     `json:"requested"`
	Succeeded    int64   `json:"succeeded"`
	Failed       int64   `json:"failed"`
	DurationSec  float64 `json:"duration_sec"`
	TPS          float64 `json:"tps"`
	AvgMs        float64 `json:"avg_ms"`
	P95Ms        float64 `json:"p95_ms"`
	MaxMs        float64 `json:"max_ms"`
	StartedAtUTC string  `json:"started_at_utc"`
}

func main() {
	var (
		addr        = flag.String("addr", "127.0.0.1:2525", "SMTP server address")
		concurrency = flag.Int("concurrency", 10, "number of workers")
		messages    = flag.Int("messages", 100, "number of messages to send")
		from        = flag.String("from", "loadtest@example.net", "envelope from")
		to          = flag.String("to", "receiver@example.net", "envelope to")
		timeout     = flag.Duration("timeout", 8*time.Second, "dial/read/write timeout")
	)
	flag.Parse()

	if *concurrency <= 0 || *messages <= 0 {
		fmt.Fprintln(os.Stderr, "concurrency and messages must be > 0")
		os.Exit(2)
	}

	started := time.Now().UTC()
	jobs := make(chan int)
	latCh := make(chan time.Duration, *messages)

	var succeeded int64
	var failed int64
	var wg sync.WaitGroup

	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range jobs {
				_ = n
				t0 := time.Now()
				err := sendOne(*addr, *from, *to, *timeout)
				if err != nil {
					atomic.AddInt64(&failed, 1)
					continue
				}
				latCh <- time.Since(t0)
				atomic.AddInt64(&succeeded, 1)
			}
		}()
	}

	begin := time.Now()
	for i := 0; i < *messages; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	close(latCh)
	elapsed := time.Since(begin)

	latencies := make([]float64, 0, len(latCh))
	for d := range latCh {
		latencies = append(latencies, float64(d.Microseconds())/1000.0)
	}
	sort.Float64s(latencies)

	s := summary{
		Address:      *addr,
		Concurrency:  *concurrency,
		Requested:    *messages,
		Succeeded:    succeeded,
		Failed:       failed,
		DurationSec:  elapsed.Seconds(),
		StartedAtUTC: started.Format(time.RFC3339),
	}

	if elapsed > 0 {
		s.TPS = float64(succeeded) / elapsed.Seconds()
	}
	if len(latencies) > 0 {
		var sum float64
		for _, v := range latencies {
			sum += v
		}
		s.AvgMs = sum / float64(len(latencies))
		s.P95Ms = percentile(latencies, 95)
		s.MaxMs = latencies[len(latencies)-1]
	}

	fmt.Printf("{\"address\":%q,\"concurrency\":%d,\"requested\":%d,\"succeeded\":%d,\"failed\":%d,\"duration_sec\":%.3f,\"tps\":%.3f,\"avg_ms\":%.3f,\"p95_ms\":%.3f,\"max_ms\":%.3f,\"started_at_utc\":%q}\n",
		s.Address, s.Concurrency, s.Requested, s.Succeeded, s.Failed, s.DurationSec, s.TPS, s.AvgMs, s.P95Ms, s.MaxMs, s.StartedAtUTC)

	if failed > 0 {
		os.Exit(1)
	}
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	rank := (p / 100.0) * float64(len(sorted)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return sorted[lo]
	}
	frac := rank - float64(lo)
	return sorted[lo] + (sorted[hi]-sorted[lo])*frac
}

func sendOne(addr, from, to string, timeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	tp := textproto.NewConn(conn)
	defer tp.Close()

	if _, _, err := tp.ReadResponse(220); err != nil {
		return err
	}
	if err := sendCmd(tp, 250, "EHLO loadgen"); err != nil {
		return err
	}
	if err := sendCmd(tp, 250, fmt.Sprintf("MAIL FROM:<%s>", from)); err != nil {
		return err
	}
	if err := sendCmd(tp, 250, fmt.Sprintf("RCPT TO:<%s>", to)); err != nil {
		return err
	}
	if err := sendCmd(tp, 354, "DATA"); err != nil {
		return err
	}

	writer := bufio.NewWriter(conn)
	msg := strings.Join([]string{
		"From: <" + from + ">",
		"To: <" + to + ">",
		"Subject: kuroshio-mta Load Test",
		"Date: " + time.Now().UTC().Format(time.RFC1123Z),
		"",
		"load test message",
		".",
		"",
	}, "\r\n")
	if _, err := writer.WriteString(msg); err != nil {
		return err
	}
	if err := writer.Flush(); err != nil {
		return err
	}

	if _, _, err := tp.ReadResponse(250); err != nil {
		return err
	}
	if err := sendCmd(tp, 221, "QUIT"); err != nil && !errors.Is(err, net.ErrClosed) {
		return err
	}
	return nil
}

func sendCmd(tp *textproto.Conn, expect int, cmd string) error {
	if err := tp.PrintfLine("%s", cmd); err != nil {
		return err
	}
	_, _, err := tp.ReadResponse(expect)
	return err
}
