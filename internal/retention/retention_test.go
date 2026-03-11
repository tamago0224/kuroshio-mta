package retention

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSweepRemovesExpiredFiles(t *testing.T) {
	root := t.TempDir()
	sentDir := filepath.Join(root, "sent")
	if err := os.MkdirAll(sentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	old := filepath.Join(sentDir, "old.json")
	newf := filepath.Join(sentDir, "new.json")
	if err := os.WriteFile(old, []byte("x"), 0o644); err != nil {
		t.Fatalf("write old: %v", err)
	}
	if err := os.WriteFile(newf, []byte("x"), 0o644); err != nil {
		t.Fatalf("write new: %v", err)
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(old, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if err := sweep(root, Policy{SentTTL: 24 * time.Hour}); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old file must be deleted: err=%v", err)
	}
	if _, err := os.Stat(newf); err != nil {
		t.Fatalf("new file must remain: %v", err)
	}
}
