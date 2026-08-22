package security

import (
	"reflect"
	"testing"
)

func TestResolveAllowedRemotesUsesAllDiscoveredWhenAllowlistIsEmpty(t *testing.T) {
	got, err := ResolveAllowedRemotes(nil, []string{"archive", "onedrive", "archive"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"archive", "onedrive"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolved remotes=%v want=%v", got, want)
	}
}

func TestResolveAllowedRemotesKeepsExplicitSubset(t *testing.T) {
	got, err := ResolveAllowedRemotes([]string{"onedrive"}, []string{"archive", "onedrive"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"onedrive"}) {
		t.Fatalf("resolved remotes=%v", got)
	}
}

func TestResolveAllowedRemotesRejectsUnknownExplicitRemote(t *testing.T) {
	if _, err := ResolveAllowedRemotes([]string{"missing"}, []string{"archive"}); err == nil {
		t.Fatal("unknown explicit remote must be rejected")
	}
}
