package main

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/InkaNode/kuraka/internal/config"
)

func healthcheckAddress(listen string) (string, error) {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		listen = config.DefaultListen
	}
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", fmt.Errorf("healthcheck address: %w", err)
	}
	return net.JoinHostPort("127.0.0.1", port), nil
}

func healthcheck(listen string) error {
	address, err := healthcheckAddress(listen)
	if err != nil {
		return err
	}
	client := &http.Client{
		Timeout:       3 * time.Second,
		Transport:     &http.Transport{Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	defer client.CloseIdleConnections()
	resp, err := client.Get("http://" + address + "/healthz")
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck: HTTP %d", resp.StatusCode)
	}
	return nil
}
