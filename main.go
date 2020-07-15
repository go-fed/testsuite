package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/go-fed/testsuite/server"
)

type CommandLineFlags struct {
	CertFile     *string
	KeyFile      *string
	Hostname     *string
	TemplateGlob *string
	TestTimeout  *time.Duration
	MaxTests     *int
}

func NewCommandLineFlags() *CommandLineFlags {
	c := &CommandLineFlags{
		CertFile:     flag.String("cert", "tls.crt", "Path to certificate public key file"),
		KeyFile:      flag.String("key", "tls.key", "Path to certificate private key file"),
		Hostname:     flag.String("host", "", "Host name of this instance (including TLD)"),
		TemplateGlob: flag.String("glob", "*.tmpl", "Glob matching the Go template files"),
		TestTimeout:  flag.Duration("test_timeout", time.Minute*15, "Maximum time tests will be kept"),
		MaxTests:     flag.Int("max_tests", 30, "Maximum number of concurrent tests"),
	}
	flag.Parse()
	if err := c.validate(); err != nil {
		panic(err)
	}
	return c
}

func (c *CommandLineFlags) validate() error {
	if len(*c.CertFile) == 0 {
		return fmt.Errorf("cert file invalid: %s", *c.CertFile)
	} else if len(*c.KeyFile) == 0 {
		return fmt.Errorf("key file invalid: %s", *c.KeyFile)
	}
	return nil
}

func main() {
	c := NewCommandLineFlags()

	tlsConfig := &tls.Config{
		MinVersion:               tls.VersionTLS12,
		CurvePreferences:         []tls.CurveID{tls.CurveP256, tls.X25519},
		PreferServerCipherSuites: true,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
	}
	httpsServer := &http.Server{
		Addr:         ":https",
		TLSConfig:    tlsConfig,
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler), 0),
	}

	tmpls, err := template.ParseGlob(*c.TemplateGlob)
	if err != nil {
		panic(err)
	}
	_ = server.NewWebServer(tmpls, httpsServer, *c.Hostname, *c.TestTimeout, *c.MaxTests)

	redir := &http.Server{
		Addr:         ":http",
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Connection", "close")
			http.Redirect(w, req, fmt.Sprintf("https://%s%s", req.Host, req.URL), http.StatusMovedPermanently)
		}),
	}
	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt)
		<-sigint
		if err := redir.Shutdown(context.Background()); err != nil {
			log.Printf("HTTP redirect server Shutdown: %v", err)
		}
		if err := httpsServer.Shutdown(context.Background()); err != nil {
			log.Printf("HTTP server Shutdown: %v", err)
		}
	}()
	go func() {
		if err := redir.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("HTTP redirect server ListenAndServe: %v", err)
		}
	}()
	if err := httpsServer.ListenAndServeTLS(*c.CertFile, *c.KeyFile); err != nil {
		panic(err)
	}
}
