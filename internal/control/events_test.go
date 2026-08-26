package control

import (
	"testing"
	"time"

	"github.com/MaimoryLab/codeoff-server/internal/appserver"
)

func TestEventHubBroadcastsToEachSubscriber(t *testing.T) {
	source := make(chan appserver.Event, 1)
	hub := newEventHub(source)
	first, unsubscribeFirst := hub.subscribe()
	defer unsubscribeFirst()
	second, unsubscribeSecond := hub.subscribe()
	defer unsubscribeSecond()
	source <- appserver.Event{Method: "test"}
	for _, subscriber := range []<-chan appserver.Event{first, second} {
		select {
		case event := <-subscriber:
			if event.Method != "test" {
				t.Fatalf("event method = %q", event.Method)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for broadcast")
		}
	}
	close(source)
}
