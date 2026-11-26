package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"online-bookmarks/internal/app"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	showVersion := flag.Bool("version", false, "print application version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("online-bookmarks " + app.Version)
		return
	}
	if e := app.LoadConfig(); e != nil {
		log.Fatal(e)
	}
	a, e := app.Open()
	if e != nil {
		log.Fatal(e)
	}
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	s := &http.Server{Addr: addr, Handler: a.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s.Shutdown(c)
	}()
	log.Printf("online-bookmarks %s listening on %s", app.Version, addr)
	if e = s.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		log.Fatal(e)
	}
}
