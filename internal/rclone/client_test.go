package rclone

import (
	"strings"
	"testing"
)

func TestParseTransferredProgress(t *testing.T) {
	progress, ok := parseProgressLine("Transferred: 1.5 MiB / 10 MiB, 15%, 2.5 MiB/s, ETA 4s")
	if !ok {
		t.Fatal("expected progress line to parse")
	}
	if progress.Percent != 15 || progress.BytesDone != 1572864 || progress.BytesTotal != 10485760 || progress.Speed != 2621440 || progress.ETA != "4s" {
		t.Fatalf("unexpected progress: %+v", progress)
	}
}

func TestParseNumericProgress(t *testing.T) {
	progress, ok := parseProgressLine("1234/5678, 21.7%, 1.2 MiB/s")
	if !ok || progress.BytesDone != 1234 || progress.BytesTotal != 5678 || progress.Percent != 21.7 {
		t.Fatalf("unexpected progress: %+v parsed=%v", progress, ok)
	}
}

func TestCappedBufferKeepsLatestDiagnostics(t *testing.T) {
	buffer := cappedBuffer{max: 8}
	buffer.Append("old-")
	buffer.Append("failure")
	if got := buffer.String(); got != "-failure" {
		t.Fatalf("expected latest diagnostics, got %q", got)
	}
	buffer.Append(strings.Repeat("x", 10))
	if got := buffer.String(); got != strings.Repeat("x", 8) {
		t.Fatalf("expected oversized value tail, got %q", got)
	}
}

func TestParseListRemotesLong(t *testing.T) {
	remotes := parseListRemotesLong("archive: s3\nbackup: drive\n")
	if len(remotes) != 2 || remotes[0].Name != "archive" || remotes[0].Type != "s3" || remotes[1].Name != "backup" {
		t.Fatalf("unexpected remotes: %+v", remotes)
	}
}
