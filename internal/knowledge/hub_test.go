package knowledge

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/tools"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func requireDoltForHubTest(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not installed")
	}
}

func TestOpenInHubServesTwoProjectsAsIsolatedDatabasesOnOneServer(t *testing.T) {
	requireDoltForHubTest(t)

	root := t.TempDir()
	hubDir := filepath.Join(root, "hub")
	sup := tools.New(root)

	storeA, err := OpenInHub(sup, hubDir, "project-a")
	if err != nil {
		t.Fatalf("OpenInHub project-a: %v", err)
	}
	t.Cleanup(func() { _ = storeA.Close() })

	storeB, err := OpenInHub(sup, hubDir, "project-b")
	if err != nil {
		t.Fatalf("OpenInHub project-b: %v", err)
	}
	t.Cleanup(func() { _ = storeB.Close() })

	recA := protocol.KnowledgeRecord{
		Id: "pkw:req/fixture/A-ONLY", Type: protocol.KnowledgeRecordTypeRequirement, Status: "active", Title: "A only",
		Source:     protocol.KnowledgeRecordSource{Provider: "test", RetrievedAt: time.Now().UTC()},
		Extraction: protocol.KnowledgeRecordExtraction{Method: protocol.KnowledgeRecordExtractionMethodManual},
		Validity:   protocol.KnowledgeRecordValidity{State: protocol.KnowledgeRecordValidityStateObserved},
	}
	if err := storeA.Put(recA); err != nil {
		t.Fatalf("Put on project-a: %v", err)
	}

	// project-b's database must not see project-a's record, even though both
	// are served by the same dolt sql-server process.
	if _, err := storeB.Get(recA.Id); err == nil {
		t.Fatal("expected project-b's store to NOT see project-a's record - they must be isolated databases")
	}
	if _, err := storeA.Get(recA.Id); err != nil {
		t.Fatalf("expected project-a's own store to see its own record: %v", err)
	}
}

func TestOpenInHubClosingOneProjectLeavesTheSharedServerRunningForAnother(t *testing.T) {
	requireDoltForHubTest(t)

	root := t.TempDir()
	hubDir := filepath.Join(root, "hub")
	sup := tools.New(root)

	storeA, err := OpenInHub(sup, hubDir, "project-a")
	if err != nil {
		t.Fatalf("OpenInHub project-a: %v", err)
	}
	storeB, err := OpenInHub(sup, hubDir, "project-b")
	if err != nil {
		t.Fatalf("OpenInHub project-b: %v", err)
	}
	t.Cleanup(func() { _ = storeB.Close() })

	// Closing project-a's store must not stop the shared hub server:
	// project-b is still using it (the shared refcount, keyed by hubDir, is
	// still > 0).
	if err := storeA.Close(); err != nil {
		t.Fatalf("close project-a: %v", err)
	}

	if err := storeB.DB().Ping(); err != nil {
		t.Fatalf("expected project-b's store to still be usable after project-a closed, got: %v", err)
	}
}

func TestOpenInHubRequiresProjectID(t *testing.T) {
	sup := tools.New(t.TempDir())
	if _, err := OpenInHub(sup, filepath.Join(t.TempDir(), "hub"), ""); err == nil {
		t.Fatal("expected an error when projectID is empty")
	}
}

func TestOpenInHubRejectsUnsafeProjectID(t *testing.T) {
	sup := tools.New(t.TempDir())
	hubDir := filepath.Join(t.TempDir(), "hub")
	for _, bad := range []string{"proj`a", "proj;drop", "proj a", "../escape", "proj/a"} {
		if _, err := OpenInHub(sup, hubDir, bad); err == nil {
			t.Fatalf("expected an error for unsafe projectID %q", bad)
		}
	}
}

func TestOpenInHubReopenAfterCloseSeesPersistedData(t *testing.T) {
	requireDoltForHubTest(t)

	root := t.TempDir()
	hubDir := filepath.Join(root, "hub")
	sup := tools.New(root)

	first, err := OpenInHub(sup, hubDir, "project-persist")
	if err != nil {
		t.Fatalf("first OpenInHub: %v", err)
	}
	rec := protocol.KnowledgeRecord{
		Id: "pkw:req/fixture/PERSIST-1", Type: protocol.KnowledgeRecordTypeRequirement, Status: "active", Title: "Persisted",
		Source:     protocol.KnowledgeRecordSource{Provider: "test", RetrievedAt: time.Now().UTC()},
		Extraction: protocol.KnowledgeRecordExtraction{Method: protocol.KnowledgeRecordExtractionMethodManual},
		Validity:   protocol.KnowledgeRecordValidity{State: protocol.KnowledgeRecordValidityStateObserved},
	}
	if err := first.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopening exercises joinHubServer's CREATE DATABASE IF NOT EXISTS path
	// against an already-populated database (the server may or may not still
	// be running depending on whether Close's PROCESSLIST check saw itself as
	// the last client - either way, the same call must work and the data
	// must still be there).
	second, err := OpenInHub(sup, hubDir, "project-persist")
	if err != nil {
		t.Fatalf("second OpenInHub: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	if _, err := second.Get(rec.Id); err != nil {
		t.Fatalf("expected the record to survive close+reopen: %v", err)
	}
}
