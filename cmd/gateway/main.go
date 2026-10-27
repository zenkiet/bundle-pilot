package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/zenkiet/bundle-pilot/internal/domain"
	"github.com/zenkiet/bundle-pilot/internal/env"
	"github.com/zenkiet/bundle-pilot/internal/gen/configv1"
	"github.com/zenkiet/bundle-pilot/internal/handler"
	"github.com/zenkiet/bundle-pilot/internal/infrastructure/bundlefs"
	"github.com/zenkiet/bundle-pilot/internal/ui"
	"github.com/zenkiet/bundle-pilot/internal/usecase"
)

func main() {
	cfg, err := env.Load(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "check":
			os.Exit(check(cfg, os.Args[2:]))
		case "import":
			os.Exit(importConfig(cfg, os.Args[2:]))
		case "health":
			os.Exit(health(cfg))
		}
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	if err := run(cfg, log); err != nil {
		log.Error("exit", "error", err)
		os.Exit(1)
	}
}

func run(cfg env.Config, log *slog.Logger) error {
	if err := os.MkdirAll(cfg.Dist, 0o755); err != nil {
		return err
	}
	cat, err := catalog(cfg, log)
	if err != nil {
		return err
	}
	if err := cat.Reload(); err != nil {
		return fmt.Errorf("load %s: %w", cfg.Dist, err)
	}
	if t := cat.Current().Setup; t != "" {
		log.Error("first-run setup: enter this token on /__gateway/ui/setup/", "token", t) // no LOG_LEVEL hides Error
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go cat.Watch(ctx, 5*time.Second)

	assets, _ := fs.Sub(ui.Build, "build/app")
	h := handler.New(cat, assets, log)
	var draining atomic.Bool
	srv := &http.Server{
		Addr: cfg.Addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if draining.Load() && r.URL.Path == "/healthz" {
				http.Error(w, "draining", http.StatusServiceUnavailable)
				return
			}
			h.ServeHTTP(w, r)
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       time.Minute,
		WriteTimeout:      time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("listening", "addr", cfg.Addr, "dist", cfg.Dist)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	stop()
	draining.Store(true)
	// ponytail: a fixed lame duck that outlasts a 2 s health probe; make it a flag if probes differ.
	time.Sleep(6 * time.Second)
	drain, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(drain); !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	log.Warn("drain timed out, closing open connections")
	return srv.Close()
}

// check loads DIST the way the server does, prints every issue and exits 1 on any error.
func check(cfg env.Config, args []string) int {
	if len(args) > 0 {
		cfg.Dist = args[0]
	}
	cat, err := catalog(cfg, slog.New(slog.DiscardHandler))
	if err == nil {
		err = cat.Reload()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	snap := cat.Current()
	for _, e := range snap.Errors {
		fmt.Println("error:", e)
	}
	for _, w := range snap.Issues {
		fmt.Println("warning:", w)
	}
	fmt.Printf("%d bundles, default %s (%s), %d rules, %d backend steps\n",
		len(snap.Bundles()), snap.Default.Version, snap.DefaultFrom, len(snap.Config.Rules), len(snap.Config.Backend))
	if len(snap.Errors) > 0 {
		return 1
	}
	return 0
}

// importConfig validates a JSON config and stores it as DIST/config.pb.
func importConfig(cfg env.Config, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: gateway import config.json [DIST]")
		return 2
	}
	if len(args) > 1 {
		cfg.Dist = args[1]
	}
	if err := writeImported(args[0], cfg.Dist); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	fmt.Printf("wrote %s/config.pb\n", cfg.Dist)
	return 0
}

// writeImported saves a JSON config file as dist's config.pb. Like a save from the UI, it keeps
// the stored admin account and secrets that the JSON leaves out.
func writeImported(file, dist string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	pb, err := domain.DecodeConfig(data, true)
	if err != nil {
		return err
	}
	src, err := bundlefs.New(dist, slog.New(slog.NewTextHandler(os.Stderr, nil)), nil)
	if err != nil {
		return err
	}
	cur, err := src.ReadConfig()
	if errors.Is(err, fs.ErrNotExist) {
		cur, err = &configv1.Config{}, nil
	}
	if err != nil {
		return err
	}
	domain.KeepStored(pb, cur)
	if _, err := domain.ParseConfig(pb); err != nil {
		return err
	}
	domain.HashAuth(pb)
	return src.WriteConfig(pb, "cli")
}

// health exits 0 while the local gateway reports healthy, for HEALTHCHECK in images without curl.
func health(cfg env.Config) int {
	client := &http.Client{Timeout: 2 * time.Second}
	res, err := client.Get("http://127.0.0.1" + cfg.Addr + "/healthz")
	if err != nil {
		return 1
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func catalog(cfg env.Config, log *slog.Logger) (*usecase.Catalog, error) {
	src, err := bundlefs.New(cfg.Dist, log, cfg.BundleKey)
	if err != nil {
		return nil, err
	}
	return usecase.NewCatalog(src, log), nil
}
