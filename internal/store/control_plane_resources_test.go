package store

import "testing"

func TestUpsertAndReadControlPlaneResource(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	resource := &ControlPlaneResource{
		Kind:         "RoutingPolicy",
		Name:         "control-plane-default",
		APIVersion:   "myassistant.dev/v1alpha1",
		SourcePath:   "/tmp/control-plane.yaml",
		SourceCommit: "abc123",
		SpecHash:     "hash123",
		RawSpecJSON:  `{"version":"2026-04-25","rules":[{"prefer":"codex"}]}`,
	}
	if err := UpsertControlPlaneResource(db, resource); err != nil {
		t.Fatalf("UpsertControlPlaneResource: %v", err)
	}

	count, err := CountControlPlaneResources(db)
	if err != nil {
		t.Fatalf("CountControlPlaneResources: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	got, err := GetControlPlaneResource(db, "RoutingPolicy", "control-plane-default")
	if err != nil {
		t.Fatalf("GetControlPlaneResource: %v", err)
	}
	if got == nil {
		t.Fatal("expected control plane resource")
	}
	if got.SpecHash != "hash123" {
		t.Fatalf("specHash = %q", got.SpecHash)
	}

	listed, err := ListControlPlaneResources(db, "RoutingPolicy")
	if err != nil {
		t.Fatalf("ListControlPlaneResources: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("len(listed) = %d, want 1", len(listed))
	}
	if listed[0].Name != "control-plane-default" {
		t.Fatalf("listed[0].Name = %q", listed[0].Name)
	}
}
