package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"rsc.io/qr"

	"github.com/zenkiet/bundle-pilot/internal/domain"
	"github.com/zenkiet/bundle-pilot/internal/env"
	"github.com/zenkiet/bundle-pilot/internal/gen/configv1"
	"github.com/zenkiet/bundle-pilot/internal/handler"
	"github.com/zenkiet/bundle-pilot/internal/infrastructure/bundlefs"
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
		case "setup":
			os.Exit(setup(cfg, os.Args[2:]))
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
		// no LOG_LEVEL hides Error
		log.Error("first-run setup: enter this token in the Bundle Pilot app (gateway setup shows it with a QR), or send it as X-Setup-Token with UpdateConfig", "token", t)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go cat.Watch(ctx, 5*time.Second)

	h := handler.New(cat, cfg.Origins, log, ctx.Done())
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
		fmt.Println("error:", e.Text)
	}
	for _, w := range snap.Issues {
		fmt.Println("warning:", w.Text)
	}
	fmt.Printf("%d bundles, default %s (%s), %d rules, %d backend steps\n",
		len(snap.Bundles()), snap.Default.Version, snap.DefaultFrom, len(snap.Config.Rules), len(snap.Config.Backend))
	if len(snap.Errors) > 0 {
		return 1
	}
	return 0
}

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

// writeImported keeps the stored admin account and secrets the JSON leaves out, like UpdateConfig.
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
	_, err = src.WriteConfig(pb, "cli", "")
	return err
}

func setup(cfg env.Config, args []string) int {
	fl := flag.NewFlagSet("setup", flag.ContinueOnError)
	addr := fl.String("url", "", "the gateway's address, for the app to add")
	if fl.Parse(args) != nil {
		return 2
	}
	if fl.NArg() > 0 {
		cfg.Dist = fl.Arg(0)
	}
	token, err := bundlefs.ReadSetupToken(cfg.Dist)
	if err != nil {
		fmt.Fprintln(os.Stderr, "no setup running in "+cfg.Dist+": it runs while the gateway has no config.pb")
		return 1
	}
	query := "setup=" + url.QueryEscape(token)
	if *addr != "" {
		query = "url=" + url.QueryEscape(*addr) + "&" + query
	}
	link := "bundlepilot://add?" + query
	fmt.Println("token:", token)
	fmt.Println("link: ", link)
	info, err := os.Stdout.Stat()
	if err == nil && info.Mode()&os.ModeCharDevice != 0 { // a terminal, not a pipe
		printQR(link)
	}
	return 0
}

// printQR draws two module rows per line, inside the 2-module border scanners need.
func printQR(text string) {
	code, err := qr.Encode(text, qr.L)
	if err != nil {
		return
	}
	blocks := [4]string{" ", "▀", "▄", "█"}
	bit := func(x, y int) int {
		if code.Black(x, y) { // false outside the code, which draws the border
			return 1
		}
		return 0
	}
	for y := -2; y < code.Size+2; y += 2 {
		line := "\x1b[30;47m" // black on white, whatever the terminal's theme
		for x := -2; x < code.Size+2; x++ {
			line += blocks[bit(x, y)+2*bit(x, y+1)]
		}
		fmt.Println(line + "\x1b[0m")
	}
}

// health serves HEALTHCHECK in images without curl.
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
