package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"translation-overlay/internal/composition"
	audiohost "translation-overlay/internal/feature/audio/host"
	_ "translation-overlay/internal/feature/mictranslate/infra"
	inboundhttp "translation-overlay/internal/httpapi"
	"translation-overlay/internal/platform/engine"
	"translation-overlay/internal/platform/lifecycle"
	"translation-overlay/internal/platform/netguard"
	"translation-overlay/internal/platform/version"
)

var appShutdownOnce sync.Once

func shutdownAll(engineStop context.CancelFunc) {
	appShutdownOnce.Do(func() {
		log.Print("shutting down translation-overlay…")

		engine.Shutdown()
		lifecycle.RunAll()
		if engineStop != nil {
			engineStop()
		}
	})
}

func main() {
	httpAddr := flag.String("http", "", "serve UI/API over HTTP only (no Wails window), e.g. :18780 (bound to loopback)")
	flag.Parse()

	dataDir, err := resolveDataDir()
	if err != nil {
		log.Fatal(err)
	}
	app, err := composition.New(dataDir)
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	inboundhttp.Mount(mux, app)

	engineCtx, engineStop := context.WithCancel(context.Background())
	defer func() { shutdownAll(engineStop) }()

	startEngine := func() {
		go func() {
			if err := engine.Prepare(engineCtx, app.SettingsRepo.Load); err != nil {
				log.Printf("engine: %v", err)
			}
		}()
	}

	if *httpAddr != "" {
		audiohost.StartNativeLoopbackSidecar(engineCtx)
		startEngine()
		go func() {
			sig := make(chan os.Signal, 1)
			signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
			<-sig
			log.Print("shutting down…")
			shutdownAll(engineStop)
			os.Exit(0)
		}()
		bind := loopbackBindAddr(*httpAddr)
		if bind != *httpAddr {
			log.Printf("note: -http %q bound to loopback %q — the HTTP API forwards stored API keys and must not be LAN-reachable", *httpAddr, bind)
		}
		log.Printf("Penguin Translate %s — HTTP on http://%s (hub: /ui/)", version.Version, bind)
		srv := &http.Server{
			Addr:              bind,
			Handler:           netguard.RequireLoopbackHost(mux),
			ReadHeaderTimeout: 10 * time.Second,
		}
		log.Fatal(srv.ListenAndServe())
	}

	err = wails.Run(&options.App{
		Title:  "Penguin Translate " + version.Version,
		Width:  1200,
		Height: 900,
		AssetServer: &assetserver.Options{
			Handler: mux,
		},
		BackgroundColour: &options.RGBA{R: 12, G: 14, B: 18, A: 255},
		OnStartup: func(ctx context.Context) {
			audiohost.StartNativeLoopbackSidecar(engineCtx)
			startEngine()
		},
		OnBeforeClose: func(_ context.Context) bool {
			shutdownAll(engineStop)
			return false
		},
		OnShutdown: func(_ context.Context) {
			shutdownAll(engineStop)
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	shutdownAll(engineStop)
}

func loopbackBindAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host, port = "", strings.TrimPrefix(addr, ":")
	}
	switch host {
	case "", "0.0.0.0", "::", "*":
		return "127.0.0.1:" + port
	default:
		return addr
	}
}

var errMissingLocalAppData = errors.New("LOCALAPPDATA is not set")

func resolveDataDir() (string, error) {
	if d := strings.TrimSpace(os.Getenv("TO_DATA_DIR")); d != "" {
		return d, nil
	}
	if runtime.GOOS == "windows" {
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			return "", errMissingLocalAppData
		}
		return filepath.Join(local, "translation-overlay"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "translation-overlay"), nil
}
