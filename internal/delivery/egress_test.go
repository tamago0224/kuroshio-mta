package delivery

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tamago0224/kuroshio-mta/internal/config"
	"github.com/tamago0224/kuroshio-mta/internal/model"
	"github.com/tamago0224/kuroshio-mta/internal/shaping"
)

func TestEgressManagerWeightedSelectionLimitAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	newManager := func() *egressManager {
		return &egressManager{sources: map[string]config.EgressSource{"a": {Name: "a", IP: "127.0.0.1", EHLO: "a.test", Weight: 2, DailyLimit: 2}, "b": {Name: "b", IP: "127.0.0.1", EHLO: "b.test", Weight: 1}}, cycles: map[string][]string{"pool": {"a", "a", "b"}}, cursors: map[string]int{}, tenantPools: map[string]string{}, path: path, usage: egressUsage{Counts: map[string]int{}}, now: func() time.Time { return now }}
	}
	m := newManager()
	for _, want := range []string{"a", "a", "b"} {
		got, err := m.selectSource("pool")
		if err != nil || got.name != want {
			t.Fatalf("selection = %+v %v, want %s", got, err, want)
		}
	}
	if _, err := m.selectSource("pool"); err == nil {
		t.Fatal("daily limit was bypassed through another source")
	} else if _, ok := err.(*shaping.DeferredError); !ok {
		t.Fatalf("limit error = %T %v", err, err)
	}
	restarted := newManager()
	dataManager, err := newEgressManagerFromState(restarted)
	if err != nil {
		t.Fatal(err)
	}
	if dataManager.usage.Counts["a"] != 2 {
		t.Fatalf("restart usage = %+v", dataManager.usage)
	}
}

func TestEgressManagerRejectsUnassignedSource(t *testing.T) {
	original := interfaceAddrs
	interfaceAddrs = func() ([]net.Addr, error) {
		_, network, err := net.ParseCIDR("192.0.2.10/32")
		return []net.Addr{network}, err
	}
	t.Cleanup(func() { interfaceAddrs = original })
	_, err := newEgressManager(config.Config{
		QueueDir:      t.TempDir(),
		EgressSources: []config.EgressSource{{Name: "missing", IP: "192.0.2.11", EHLO: "mail.example.test", Weight: 1}},
		EgressPools:   []config.EgressPool{{Name: "default", Sources: []string{"missing"}}},
	})
	if err == nil || !strings.Contains(err.Error(), "not assigned") {
		t.Fatalf("newEgressManager error = %v", err)
	}
}

func TestEgressManagerDoesNotBypassStoppedSource(t *testing.T) {
	m := &egressManager{
		sources: map[string]config.EgressSource{
			"stopped": {Name: "stopped", Stopped: true},
			"ready":   {Name: "ready"},
		},
		cycles: map[string][]string{"pool": {"stopped", "ready"}}, cursors: map[string]int{},
		usage: egressUsage{Counts: map[string]int{}}, now: time.Now,
	}
	if _, err := m.selectSource("pool"); err == nil {
		t.Fatal("stopped source was bypassed")
	} else if _, ok := err.(*shaping.DeferredError); !ok {
		t.Fatalf("stopped source error = %T %v", err, err)
	}
}

func TestEgressManagerNormalizesTenantPoolLookup(t *testing.T) {
	m := &egressManager{tenantPools: map[string]string{"alice@example.test": "transactional"}}
	if got := m.poolFor("route-default", " Alice@Example.Test "); got != "transactional" {
		t.Fatalf("poolFor = %q", got)
	}
}

func newEgressManagerFromState(m *egressManager) (*egressManager, error) {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &m.usage); err != nil {
		return nil, err
	}
	return m, nil
}

func TestDeliverHostUsesConfiguredSourceAndEHLO(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	result := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			result <- err
			return
		}
		defer conn.Close()
		if !strings.HasPrefix(conn.RemoteAddr().String(), "127.0.0.1:") {
			result <- fmt.Errorf("remote=%s", conn.RemoteAddr())
			return
		}
		r := bufio.NewReader(conn)
		fmt.Fprint(conn, "220 ready\r\n")
		line, _ := r.ReadString('\n')
		if line != "EHLO source.example.test\r\n" {
			result <- fmt.Errorf("EHLO=%q", line)
			return
		}
		fmt.Fprint(conn, "250 hello\r\n")
		for _, response := range []string{"250 ok\r\n", "250 ok\r\n", "354 data\r\n"} {
			if _, err := r.ReadString('\n'); err != nil {
				result <- err
				return
			}
			fmt.Fprint(conn, response)
		}
		for {
			line, err = r.ReadString('\n')
			if err != nil {
				result <- err
				return
			}
			if line == ".\r\n" {
				break
			}
		}
		fmt.Fprint(conn, "250 queued\r\n")
		_, _ = r.ReadString('\n')
		fmt.Fprint(conn, "221 bye\r\n")
		result <- nil
	}()
	client := NewClient(config.Config{Hostname: "default.test", DialTimeout: time.Second, SendTimeout: time.Second})
	ctx := context.WithValue(context.Background(), egressContextKey{}, egressSelection{name: "source", ip: "127.0.0.1", ehlo: "source.example.test"})
	err = client.deliverHost(ctx, "127.0.0.1", listener.Addr().(*net.TCPAddr).Port, &model.Message{MailFrom: "sender@example.test", Data: []byte("From: sender@example.test\r\n\r\nbody")}, "user@example.net", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestDeliverHostRejectsAddressFamilyMismatch(t *testing.T) {
	client := NewClient(config.Config{Hostname: "default.test", DialTimeout: time.Second, SendTimeout: time.Second})
	ctx := context.WithValue(context.Background(), egressContextKey{}, egressSelection{name: "source-v6", ip: "::1", ehlo: "source.example.test"})
	err := client.deliverHost(ctx, "127.0.0.1", 25, &model.Message{MailFrom: "sender@example.test"}, "user@example.net", false, nil)
	if err == nil || !strings.Contains(err.Error(), "address families do not match") {
		t.Fatalf("deliverHost error = %v", err)
	}
}
