package bus

import (
	"testing"
	"time"
)

func TestPublishReachesAllAndCoalesces(t *testing.T) {
	var b Bus
	a, cancelA := b.Subscribe()
	c, cancelC := b.Subscribe()
	defer cancelC()

	b.Publish()
	b.Publish() // coalesced with the first
	for name, ch := range map[string]<-chan struct{}{"a": a, "c": c} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("%s: no signal", name)
		}
		select {
		case <-ch:
			t.Errorf("%s: second signal, want coalesced", name)
		default:
		}
	}

	cancelA()
	b.Publish()
	select {
	case <-a:
		t.Error("unsubscribed channel still receives")
	default:
	}
}

func TestPublishWithoutSubscribers(t *testing.T) {
	var b Bus
	b.Publish() // must not panic or block
}
