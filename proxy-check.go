package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	URL "net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mmpx12/optionparser"
	"golang.org/x/net/proxy"
)

const version = "2.0.0"

const (
	defaultTargetURL    = "http://checkip.amazonaws.com"
	defaultListenAddr   = ":8080"
	defaultTimeoutSec   = 3
	defaultConcurrency  = 50
	defaultMaxProxies   = 1000
	maxRequestBodyBytes = 1 << 20
)

type CheckRequest struct {
	Proxies     []string `json:"proxies"`
	TargetURL   string   `json:"target_url"`
	TimeoutSec  int      `json:"timeout_sec"`
	Concurrency int      `json:"concurrency"`
}

type ProxyCheckResult struct {
	Proxy       string `json:"proxy"`
	Available   bool   `json:"available"`
	StatusCode  int    `json:"status_code,omitempty"`
	Error       string `json:"error,omitempty"`
	DurationMS  int64  `json:"duration_ms"`
	ProxyScheme string `json:"proxy_scheme"`
}

type CheckResponse struct {
	TargetURL   string             `json:"target_url"`
	Total       int                `json:"total"`
	Available   int                `json:"available"`
	Unavailable int                `json:"unavailable"`
	Results     []ProxyCheckResult `json:"results"`
}

type healthResponse struct {
	OK      bool   `json:"ok"`
	Version string `json:"version"`
}

func main() {
	var serverMode bool
	var listenAddr string
	var printVersion bool

	op := optionparser.NewOptionParser()
	op.Banner = "Proxy checker service\n\nUsage:\n"
	op.On("-s", "--server", "Run as HTTP service", &serverMode)
	op.On("-l", "--listen ADDR", "HTTP listen address", &listenAddr)
	op.On("-v", "--version", "Print version and exit", &printVersion)
	err := op.Parse()
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	if printVersion {
		fmt.Println("version:", version)
		return
	}

	if !serverMode {
		op.Help()
		return
	}

	if listenAddr == "" {
		listenAddr = defaultListenAddr
	}

	server := newHTTPServer()
	log.Printf("proxy-check service listening on %s", listenAddr)
	if err := http.ListenAndServe(listenAddr, server); err != nil {
		log.Fatal(err)
	}
}

func newHTTPServer() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealth)
	mux.HandleFunc("/check", handleCheck)
	return mux
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{OK: true, Version: version})
}

func handleCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	defer r.Body.Close()

	var req CheckRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	resp, err := checkProxies(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func checkProxies(ctx context.Context, req CheckRequest) (*CheckResponse, error) {
	proxies := normalizeProxies(req.Proxies)
	if len(proxies) == 0 {
		return nil, errors.New("proxies is required")
	}
	if len(proxies) > defaultMaxProxies {
		return nil, fmt.Errorf("too many proxies: max %d", defaultMaxProxies)
	}

	targetURL := strings.TrimSpace(req.TargetURL)
	if targetURL == "" {
		targetURL = defaultTargetURL
	}
	parsedTarget, err := normalizeTargetURL(targetURL)
	if err != nil {
		return nil, err
	}

	timeoutSec := req.TimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = defaultTimeoutSec
	}

	concurrency := req.Concurrency
	if concurrency <= 0 {
		concurrency = defaultConcurrency
	}
	if concurrency > len(proxies) {
		concurrency = len(proxies)
	}

	results := make([]ProxyCheckResult, len(proxies))
	guard := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, proxyAddress := range proxies {
		guard <- struct{}{}
		wg.Add(1)
		go func(index int, proxyAddress string) {
			defer wg.Done()
			defer func() { <-guard }()
			results[index] = testProxy(ctx, proxyAddress, parsedTarget, timeoutSec)
		}(i, proxyAddress)
	}

	wg.Wait()
	sort.Slice(results, func(i, j int) bool {
		return results[i].Proxy < results[j].Proxy
	})

	available := 0
	for _, result := range results {
		if result.Available {
			available++
		}
	}

	return &CheckResponse{
		TargetURL:   parsedTarget,
		Total:       len(results),
		Available:   available,
		Unavailable: len(results) - available,
		Results:     results,
	}, nil
}

func testProxy(parent context.Context, proxyAddress, targetURL string, timeoutSec int) ProxyCheckResult {
	startedAt := time.Now()
	result := ProxyCheckResult{Proxy: proxyAddress}

	proxyURL, err := URL.Parse(proxyAddress)
	if err != nil {
		result.Error = "invalid proxy url"
		result.DurationMS = time.Since(startedAt).Milliseconds()
		return result
	}

	scheme := strings.ToLower(proxyURL.Scheme)
	result.ProxyScheme = scheme
	if scheme != "http" && scheme != "socks5" {
		result.Error = "unsupported proxy scheme"
		result.DurationMS = time.Since(startedAt).Milliseconds()
		return result
	}

	ctx, cancel := context.WithTimeout(parent, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	transport, err := buildTransport(proxyURL)
	if err != nil {
		result.Error = err.Error()
		result.DurationMS = time.Since(startedAt).Milliseconds()
		return result
	}

	client := &http.Client{Transport: transport}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		result.Error = "failed to create request"
		result.DurationMS = time.Since(startedAt).Milliseconds()
		return result
	}
	req.Header.Set("User-Agent", "proxy-check-service/2.0")

	resp, err := client.Do(req)
	if err != nil {
		result.Error = err.Error()
		result.DurationMS = time.Since(startedAt).Milliseconds()
		return result
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode
	result.Available = resp.StatusCode == http.StatusOK
	if !result.Available {
		result.Error = fmt.Sprintf("unexpected status code: %d", resp.StatusCode)
	}
	result.DurationMS = time.Since(startedAt).Milliseconds()
	return result
}

func buildTransport(proxyURL *URL.URL) (*http.Transport, error) {
	baseTransport := &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}

	switch strings.ToLower(proxyURL.Scheme) {
	case "http":
		baseTransport.Proxy = http.ProxyURL(proxyURL)
		return baseTransport, nil
	case "socks5":
		dialer, err := proxy.FromURL(proxyURL, &net.Dialer{Timeout: 10 * time.Second})
		if err != nil {
			return nil, fmt.Errorf("failed to build socks5 dialer: %w", err)
		}
		baseTransport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialWithContext(ctx, dialer, network, addr)
		}
		return baseTransport, nil
	default:
		return nil, errors.New("unsupported proxy scheme")
	}
}

func dialWithContext(ctx context.Context, dialer proxy.Dialer, network, addr string) (net.Conn, error) {
	type dialResult struct {
		conn net.Conn
		err  error
	}
	resultCh := make(chan dialResult, 1)
	go func() {
		conn, err := dialer.Dial(network, addr)
		resultCh <- dialResult{conn: conn, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resultCh:
		return result.conn, result.err
	}
}

func normalizeProxies(values []string) []string {
	proxies := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		proxyAddress := strings.TrimSpace(value)
		if proxyAddress == "" {
			continue
		}
		if _, ok := seen[proxyAddress]; ok {
			continue
		}
		seen[proxyAddress] = struct{}{}
		proxies = append(proxies, proxyAddress)
	}
	return proxies
}

func normalizeTargetURL(raw string) (string, error) {
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "http://" + raw
	}
	parsed, err := URL.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "", errors.New("invalid target_url")
	}
	return parsed.String(), nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
