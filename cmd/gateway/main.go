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
	"syscall"
	"time"

	"github.com/zenkiet/bundle-pilot/internal/config"
	"github.com/zenkiet/bundle-pilot/internal/domain"
	"github.com/zenkiet/bundle-pilot/internal/handler"
	"github.com/zenkiet/bundle-pilot/internal/infrastructure/bundlefs"
	"github.com/zenkiet/bundle-pilot/internal/ui"
	"github.com/zenkiet/bundle-pilot/internal/usecase"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "check":
			os.Exit(check(os.Args[2:]))
		case "import":
			os.Exit(importConfig(os.Args[2:]))
		}
	}
	var lvl slog.LevelVar
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: &lvl}))
	if err := run(log, &lvl); err != nil {
		log.Error("exit", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, lvl *slog.LevelVar) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	lvl.Set(cfg.LogLevel)
	cat := catalog(cfg, log)
	if err := cat.Reload(); err != nil {
		return fmt.Errorf("load %s: %w", cfg.Dist, err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go cat.Watch(ctx, 5*time.Second)

	assets, _ := fs.Sub(ui.Build, "build")
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler.New(cat, assets, log),
		ReadHeaderTimeout: 5 * time.Second,
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
	drain, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(drain); !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	log.Warn("drain timed out, closing open connections")
	return srv.Close()
}

// check loads DIST the way the server does, prints every issue and exits 1 on any error.
func check(args []string) int {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if len(args) > 0 {
		cfg.Dist = args[0]
	}
	cat := catalog(cfg, slog.New(slog.DiscardHandler))
	if err := cat.Reload(); err != nil {
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
func importConfig(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: gateway import config.json [DIST]")
		return 2
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if len(args) > 1 {
		cfg.Dist = args[1]
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	pb, err := domain.DecodeConfig(data, true)
	if err == nil {
		_, err = domain.ParseConfig(pb)
	}
	if err == nil {
		err = bundlefs.New(cfg.Dist, slog.New(slog.DiscardHandler), nil).WriteConfig(pb)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	fmt.Printf("wrote %s/config.pb\n", cfg.Dist)
	return 0
}

func catalog(cfg config.Config, log *slog.Logger) *usecase.Catalog {
	return usecase.NewCatalog(bundlefs.New(cfg.Dist, log, cfg.BundleKey), log)
}
