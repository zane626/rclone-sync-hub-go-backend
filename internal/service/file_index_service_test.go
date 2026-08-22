package service

import "testing"

func TestNormalizeIndexPath(t *testing.T) {
	for input, expected := range map[string]string{"": "", "/": "", "folder\\nested/": "folder/nested", "folder/./file": "folder/file"} {
		got, err := normalizeIndexPath(input)
		if err != nil || got != expected {
			t.Fatalf("normalizeIndexPath(%q)=%q,%v want %q", input, got, err, expected)
		}
	}
	if _, err := normalizeIndexPath("../../secret"); err == nil {
		t.Fatal("path traversal must be rejected")
	}
}
