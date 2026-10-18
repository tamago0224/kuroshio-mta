//go:build integration

package main

import (
	"bufio"
	"flag"
	"fmt"
	"net"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestConfigReloadProcess(t *testing.T) {
	if path := os.Getenv("KUROSHIO_TEST_RELOAD_PROCESS"); path != "" {
		flag.CommandLine = flag.NewFlagSet("kuroshio", flag.ExitOnError)
		os.Args = []string{"kuroshio", "-config", path}
		main()
		os.Exit(0)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	reservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	smtpAddress := reservation.Addr().String()
	if err := reservation.Close(); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("listen_addr: %q\nobservability_addr: '127.0.0.1:0'\ndelivery_mode: local_spool\nqueue_dir: %s\nlocal_spool_dir: %s\n", smtpAddress, filepath.Join(dir, "queue"), filepath.Join(dir, "spool"))
	write := func(extra string) {
		t.Helper()
		pending := path + ".new"
		if err := os.WriteFile(pending, []byte(body+extra), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(pending, path); err != nil {
			t.Fatal(err)
		}
	}
	write("log_level: info\n")
	outputPath := filepath.Join(dir, "process.log")
	output, err := os.Create(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestConfigReloadProcess$")
	cmd.Env = append(os.Environ(), "KUROSHIO_TEST_RELOAD_PROCESS="+path)
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := false
	defer func() {
		if !stopped {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	waitLog := func(value string) string {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			data, _ := os.ReadFile(outputPath)
			if strings.Contains(string(data), value) {
				return string(data)
			}
			time.Sleep(10 * time.Millisecond)
		}
		data, _ := os.ReadFile(outputPath)
		t.Fatalf("missing log %s: %s", value, data)
		return ""
	}
	waitLog("smtp listening")
	connection, err := net.DialTimeout("tcp", smtpAddress, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(10 * time.Second))
	responses := textproto.NewReader(bufio.NewReader(connection))
	if _, _, err := responses.ReadResponse(220); err != nil {
		t.Fatal(err)
	}
	checkSession := func() {
		t.Helper()
		if _, err := fmt.Fprint(connection, "NOOP\r\n"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := responses.ReadResponse(250); err != nil {
			t.Fatalf("session interrupted by reload: %v", err)
		}
	}
	checkSession()
	write("log_level: debug\n")
	if err := cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	waitLog(`"config_generation":2`)
	checkSession()
	write("log_level: info\nworker_count: 9\n")
	if err := cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	waitLog("config_reload_rejected")
	checkSession()
	write("log_level: info\n")
	if err := cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	waitLog(`"config_generation":3`)
	checkSession()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		stopped = true
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown timed out")
	}
}
