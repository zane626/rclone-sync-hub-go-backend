package service

import (
	"path/filepath"
	"testing"
)

func TestLocalPathsOverlap(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "nested", "files")
	sibling := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-other")
	if !localPathsOverlap(root, child) || !localPathsOverlap(child, root) {
		t.Fatal("parent and child watch paths must overlap in both argument orders")
	}
	if !localPathsOverlap(root, root) {
		t.Fatal("identical watch paths must overlap")
	}
	if localPathsOverlap(root, sibling) {
		t.Fatal("sibling watch paths must not overlap")
	}
}
