package vrchatosc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"translation-overlay/internal/feature/mictranslate/infra/plugin"
)

func TestHoldForReadingTime(t *testing.T) {
	if got := holdFor("123456789012345678901234567890", 15, 1.5, 7); got != 2*time.Second {
		t.Errorf("latin reading time = %v, want 2s", got)
	}
	if got := holdFor("hi", 15, 1.5, 7); got != 1500*time.Millisecond {
		t.Errorf("min floor = %v, want 1.5s", got)
	}
	long := make([]byte, 1000)
	for i := range long {
		long[i] = 'a'
	}
	if got := holdFor(string(long), 15, 1.5, 7); got != 7*time.Second {
		t.Errorf("max cap = %v, want 7s", got)
	}
}

func TestHoldForCJKIsSlower(t *testing.T) {
	latin := holdFor("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 15, 0, 60)
	cjk := holdFor("ああああああああああああああああああああああああああああああ", 15, 0, 60)
	if cjk <= latin {
		t.Fatalf("cjk hold %v should exceed latin hold %v", cjk, latin)
	}
	if cjk != 4*time.Second {
		t.Errorf("cjk hold = %v, want 4s", cjk)
	}
}

func TestHoldForDefaultsOnBadCPS(t *testing.T) {
	if got := holdFor("hello world", 0, 0, 60); got != holdFor("hello world", defaultPaceCPS, 0, 60) {
		t.Errorf("cps<=0 should fall back to default: %v", got)
	}
}

func TestCapQueueDropsOldest(t *testing.T) {
	q := []paceItem{{text: "a"}, {text: "b"}, {text: "c"}, {text: "d"}}
	got := capQueue(q, 2)
	if len(got) != 2 || got[0].text != "c" || got[1].text != "d" {
		t.Fatalf("capQueue kept %+v, want the two most recent (c, d)", got)
	}
	if capQueue(q, 10); len(capQueue(q, 10)) != 4 {
		t.Fatalf("capQueue under the cap should be unchanged")
	}
}

func TestPacerOrdersAndPaces(t *testing.T) {
	const hold = 20 * time.Millisecond
	got := make(chan string, 3)
	p := &pacer{send: func(it paceItem) { got <- it.text }}

	start := time.Now()
	p.enqueue(paceItem{text: "first", hold: hold})
	p.enqueue(paceItem{text: "second", hold: hold})
	p.enqueue(paceItem{text: "third", hold: hold})

	order := make([]string, 0, 3)
	for range 3 {
		select {
		case s := <-got:
			order = append(order, s)
		case <-time.After(time.Second):
			t.Fatalf("timed out; got %v", order)
		}
	}
	elapsed := time.Since(start)

	want := []string{"first", "second", "third"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
	if elapsed < 2*hold {
		t.Errorf("elapsed %v too short; pacing not applied (want >= %v)", elapsed, 2*hold)
	}
}

func TestHandleSendsPacedOSC(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	port := conn.LocalAddr().(*net.UDPAddr).Port

	p := &Plugin{}
	cfg := fmt.Sprintf(`{"enabled":true,"on_translation":true,"notification":false,"host":"127.0.0.1","port":%d,"pace_cps":1000,"pace_min_seconds":0.05,"pace_max_seconds":1}`, port)
	if err := p.ApplyConfig(json.RawMessage(cfg)); err != nil {
		t.Fatal(err)
	}

	want := []string{"first", "second", "third"}
	start := time.Now()
	for _, s := range want {
		if err := p.Handle(context.Background(), plugin.Event{
			Type:        plugin.EventTranslationReady,
			Translation: &plugin.TranslationPayload{Target: s},
		}); err != nil {
			t.Fatalf("Handle(%q): %v", s, err)
		}
	}

	buf := make([]byte, 2048)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for i, w := range want {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			t.Fatalf("read packet %d: %v", i, err)
		}
		if !bytes.Contains(buf[:n], []byte("/chatbox/input")) {
			t.Errorf("packet %d not a chatbox message", i)
		}
		if !bytes.Contains(buf[:n], []byte(w)) {
			t.Errorf("packet %d = %q, want it to contain %q", i, buf[:n], w)
		}
	}
	if elapsed := time.Since(start); elapsed < 80*time.Millisecond {
		t.Errorf("three sends took %v; pacing not applied", elapsed)
	}
}
