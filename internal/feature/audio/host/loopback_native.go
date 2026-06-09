package host

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"translation-overlay/internal/platform/netguard"
)

var loopbackWSUpgrader = websocket.Upgrader{
	CheckOrigin: netguard.AllowBrowserOrigin,
}

func handleNativeLoopbackWS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	conn, err := loopbackWSUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("loopback ws upgrade: %v", err)
		return
	}
	defer conn.Close()
	conn.SetReadLimit(1 << 20)

	// gorilla/websocket allows only one concurrent writer; every write must hold
	// this mutex or the conn panics with "concurrent write to websocket connection".
	var writeMu sync.Mutex
	writeMsg := func(messageType int, data []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteMessage(messageType, data)
	}

	var (
		pcmQ     chan []byte
		pumpStop chan struct{}
		pumpWG   sync.WaitGroup
	)

	stopPump := func() {
		if pumpStop != nil {
			close(pumpStop)
			pumpWG.Wait()
			pumpStop = nil
		}
		if pcmQ != nil {
			nativeUnsubscribeLoopback(pcmQ)
			pcmQ = nil
		}
	}
	defer stopPump()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var cmd struct {
			Cmd      string `json:"cmd"`
			DeviceID string `json:"device_id"`
			Device   string `json:"device"`
		}
		if err := json.Unmarshal(msg, &cmd); err != nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(cmd.Cmd)) {
		case "start":
			deviceID := strings.TrimSpace(cmd.DeviceID)
			if deviceID == "" {
				deviceID = strings.TrimSpace(cmd.Device)
			}
			stopPump()
			nativeStopLoopbackCapture()
			if err := nativeStartLoopbackCapture(deviceID); err != nil {
				_ = writeMsg(websocket.TextMessage, loopbackErrorJSON(err.Error()))
				continue
			}
			time.Sleep(600 * time.Millisecond)
			if errMsg := nativeLoopbackCaptureError(); errMsg != "" {
				_ = writeMsg(websocket.TextMessage, loopbackErrorJSON(errMsg))
				continue
			}
			if !nativeLoopbackCaptureRunning() {
				_ = writeMsg(websocket.TextMessage, loopbackErrorJSON("loopback capture exited immediately"))
				continue
			}
			pcmQ = nativeSubscribeLoopback()
			pumpStop = make(chan struct{})
			pumpWG.Add(1)
			go func(q chan []byte, stop <-chan struct{}) {
				defer pumpWG.Done()
				for {
					select {
					case <-stop:
						return
					case chunk, ok := <-q:
						if !ok {
							return
						}
						if chunk == nil {
							errMsg := nativeLoopbackCaptureError()
							if errMsg != "" {
								_ = writeMsg(websocket.TextMessage, loopbackCaptureLostJSON(errMsg))
							}
							return
						}
						if err := writeMsg(websocket.BinaryMessage, chunk); err != nil {
							log.Printf("loopback native write: %v", err)
							return
						}
					}
				}
			}(pcmQ, pumpStop)
			ack, _ := loopbackStartAckJSON()
			_ = writeMsg(websocket.TextMessage, ack)
		case "stop":
			stopPump()
			nativeStopLoopbackCapture()
			_ = writeMsg(websocket.TextMessage, []byte(`{"status":"stopped"}`))
		}
	}
}
