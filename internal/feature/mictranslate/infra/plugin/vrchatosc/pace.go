package vrchatosc

import (
	"log"
	"sync"
	"time"
	"unicode"
)

const (
	defaultPaceCPS = 15.0
	defaultPaceMin = 1.5
	defaultPaceMax = 7.0
	cjkSlowdown    = 2.0
	maxPaceQueue   = 16
)

type paceItem struct {
	host         string
	port         int
	text         string
	notification bool
	hold         time.Duration
}

type pacer struct {
	mu    sync.Mutex
	queue []paceItem
	wake  chan struct{}
	once  sync.Once
	send  func(paceItem)
}

func (p *pacer) enqueue(it paceItem) {
	p.once.Do(func() {
		p.wake = make(chan struct{}, 1)
		if p.send == nil {
			p.send = defaultPaceSend
		}
		go p.run()
	})
	p.mu.Lock()
	p.queue = capQueue(append(p.queue, it), maxPaceQueue)
	p.mu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *pacer) run() {
	for {
		p.mu.Lock()
		if len(p.queue) == 0 {
			p.mu.Unlock()
			<-p.wake
			continue
		}
		it := p.queue[0]
		p.queue = p.queue[1:]
		p.mu.Unlock()

		p.send(it)
		time.Sleep(it.hold)
	}
}

func defaultPaceSend(it paceItem) {
	if err := sendRaw(it.host, it.port, it.text, it.notification); err != nil {
		log.Printf("vrchat_osc: %v", err)
	}
}

func capQueue(q []paceItem, max int) []paceItem {
	if len(q) > max {
		return q[len(q)-max:]
	}
	return q
}

func holdFor(text string, cps, minSec, maxSec float64) time.Duration {
	if cps <= 0 {
		cps = defaultPaceCPS
	}
	var other, cjk float64
	for _, r := range text {
		if isCJK(r) {
			cjk++
		} else {
			other++
		}
	}
	secs := other/cps + cjk/(cps/cjkSlowdown)
	if minSec > 0 && secs < minSec {
		secs = minSec
	}
	if maxSec > 0 && secs > maxSec {
		secs = maxSec
	}
	return time.Duration(secs * float64(time.Second))
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}
