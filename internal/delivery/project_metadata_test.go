package delivery

import (
	"context"
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestMergeProjectMetadataMergesFieldByFieldWithoutClobbering(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	created, err := s.UpsertProject(ctx, "upsert-1", NewID(), "widgets", "https://github.com/acme/widgets.git", "main")
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	if created.Metadata != nil {
		t.Fatalf("Metadata = %+v, want nil for a freshly created project", created.Metadata)
	}

	packageManager := "pnpm"
	after1, err := s.MergeProjectMetadata(ctx, "merge-1", created.Id, protocol.DeliveryProjectMetadata{
		PackageManager: &packageManager,
	})
	if err != nil {
		t.Fatalf("MergeProjectMetadata (1): %v", err)
	}
	if after1.Metadata == nil || after1.Metadata.PackageManager == nil || *after1.Metadata.PackageManager != "pnpm" {
		t.Fatalf("Metadata after first merge = %+v, want package_manager=pnpm", after1.Metadata)
	}

	layout := "monorepo"
	after2, err := s.MergeProjectMetadata(ctx, "merge-2", created.Id, protocol.DeliveryProjectMetadata{
		Layout:  &layout,
		Linters: []string{"eslint", "golangci-lint"},
	})
	if err != nil {
		t.Fatalf("MergeProjectMetadata (2): %v", err)
	}
	if after2.Metadata == nil || after2.Metadata.PackageManager == nil || *after2.Metadata.PackageManager != "pnpm" {
		t.Fatalf("Metadata after second merge = %+v, want package_manager still pnpm (not clobbered)", after2.Metadata)
	}
	if after2.Metadata.Layout == nil || *after2.Metadata.Layout != "monorepo" {
		t.Fatalf("Metadata after second merge = %+v, want layout=monorepo", after2.Metadata)
	}
	if len(after2.Metadata.Linters) != 2 {
		t.Fatalf("Metadata after second merge = %+v, want 2 linters", after2.Metadata.Linters)
	}

	fetched, err := s.GetProject(ctx, created.Id)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if fetched.Metadata == nil || fetched.Metadata.PackageManager == nil || *fetched.Metadata.PackageManager != "pnpm" || fetched.Metadata.Layout == nil || *fetched.Metadata.Layout != "monorepo" {
		t.Fatalf("GetProject metadata = %+v, want both package_manager and layout to persist across a fresh read", fetched.Metadata)
	}
}

func TestMergeProjectMetadataFailsClosedForUnknownProject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	packageManager := "npm"
	if _, err := s.MergeProjectMetadata(ctx, "merge-unknown", "not-a-real-project-id", protocol.DeliveryProjectMetadata{PackageManager: &packageManager}); err != ErrNotFound {
		t.Fatalf("MergeProjectMetadata for unknown project id: err = %v, want ErrNotFound", err)
	}
}
