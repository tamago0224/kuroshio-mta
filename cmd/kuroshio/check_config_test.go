package main

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckConfigCommand(t *testing.T) {
	if path := os.Getenv("KUROSHIO_TEST_CHECK_CONFIG"); path != "" {
		flag.CommandLine = flag.NewFlagSet("kuroshio", flag.ExitOnError)
		os.Args = []string{"kuroshio", "-check-config", "-config", path}
		main()
		os.Exit(0)
	}
	for _, tt := range []struct {
		name, body string
		success    bool
	}{
		{"valid", "delivery_mode: local_spool\n", true},
		{"invalid", "worker_count: sensitive-canary-value\n", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			queue := filepath.Join(dir, "queue")
			if err := os.WriteFile(path, []byte(tt.body+"queue_dir: "+queue+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestCheckConfigCommand$")
			cmd.Env = append(os.Environ(), "KUROSHIO_TEST_CHECK_CONFIG="+path)
			output, err := cmd.CombinedOutput()
			if (err == nil) != tt.success {
				t.Fatalf("exit mismatch: %v, %s", err, output)
			}
			if tt.success && strings.TrimSpace(string(output)) != "configuration OK" {
				t.Fatalf("unexpected output: %s", output)
			}
			if strings.Contains(string(output), "sensitive-canary-value") {
				t.Fatal("secret value printed")
			}
			if _, err := os.Stat(queue); !os.IsNotExist(err) {
				t.Fatal("check-config created queue")
			}
		})
	}
}
