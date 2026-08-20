package events

import (
	"testing"
	"time"
)

func TestHubPublishesTimestampedEvent(t *testing.T) {
	hub := NewHub(1)
	channel, unsubscribe, err := hub.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	hub.Publish(Event{Type: "task_status", TaskID: 42})
	select {
	case event := <-channel:
		if event.TaskID != 42 || event.Timestamp.IsZero() {
			t.Fatalf("unexpected event: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("event was not published")
	}
}

func TestHubEnforcesSubscriberLimitAndReleasesSlot(t *testing.T) {
	hub := NewHub(1)
	_, unsubscribe, err := hub.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := hub.Subscribe(); err != ErrSubscriberLimit {
		t.Fatalf("second subscription error=%v, want subscriber limit", err)
	}
	unsubscribe()
	_, cleanup, err := hub.Subscribe()
	if err != nil {
		t.Fatalf("subscription after cleanup: %v", err)
	}
	cleanup()
}

func TestHubDoesNotBlockOnSlowSubscriber(t *testing.T) {
	hub := NewHub(1)
	_, unsubscribe, err := hub.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			hub.Publish(Event{Type: "task_progress", TaskID: uint(i)})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("slow subscriber blocked publisher")
	}
}
