package main

import (
	"context"
	"flag"
	"log"
	"mikrodash/extensions/traffic"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:3083", "gateway listen address")
	upstream := flag.String("upstream", "http://127.0.0.1:3081", "private MikroDash origin")
	data := flag.String("data", "./traffic-data", "independent extension data directory")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	db, e := traffic.OpenDB(*data)
	if e != nil {
		log.Fatal("Cannot open extension storage: ", e)
	}
	defer db.Close()
	m, e := traffic.NewManager(ctx, db)
	if e != nil {
		log.Fatal("Cannot read extension configuration")
	}
	defer m.Close()
	handler, e := traffic.NewServer(m, *upstream)
	if e != nil {
		log.Fatal(e)
	}
	srv := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 1 << 16}
	go func() {
		<-ctx.Done()
		c, stop := context.WithTimeout(context.Background(), 8*time.Second)
		defer stop()
		_ = srv.Shutdown(c)
	}()
	go func() {
		for {
			if e := db.Prune(time.Now()); e != nil {
				log.Print("Traffic retention sweep failed")
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(24 * time.Hour):
			}
		}
	}()
	log.Printf("Traffic extension gateway listening on %s (no router configuration writes)", *listen)
	if e = srv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		log.Fatal(e)
	}
}
