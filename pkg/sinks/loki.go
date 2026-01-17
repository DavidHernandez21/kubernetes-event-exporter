package sinks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/resmoio/kubernetes-event-exporter/pkg/kube"
	"github.com/rs/zerolog/log"
)

type promtailStream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"`
}

type LokiMsg struct {
	Streams []promtailStream `json:"streams"`
}

type LokiConfig struct {
	Layout       map[string]any    `yaml:"layout"`
	StreamLabels map[string]string `yaml:"streamLabels"`
	Headers      map[string]string `yaml:"headers"`
	URL          string            `yaml:"url"`
	TLS          TLS               `yaml:"tls"`
}

type Loki struct {
	cfg       *LokiConfig
	transport *http.Transport
	client    *http.Client
}

func NewLoki(cfg *LokiConfig) (Sink, error) {
	tlsClientConfig, err := setupTLS(&cfg.TLS)
	if err != nil {
		return nil, fmt.Errorf("failed to setup TLS: %w", err)
	}

	transport := &http.Transport{
		Proxy:           http.ProxyFromEnvironment,
		TLSClientConfig: tlsClientConfig,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}

	return &Loki{cfg: cfg, transport: transport, client: client}, nil
}

func generateTimestamp() string {
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}

func (l *Loki) Send(ctx context.Context, ev *kube.EnhancedEvent) error {
	eventBody, err := serializeEventWithLayout(l.cfg.Layout, ev)
	if err != nil {
		return err
	}
	timestamp := generateTimestamp()
	a := LokiMsg{
		Streams: []promtailStream{{
			Stream: l.cfg.StreamLabels,
			Values: [][]string{{timestamp, string(eventBody)}},
		}},
	}
	reqBody, err := json.Marshal(a)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.cfg.URL, bytes.NewReader(reqBody))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	for k, v := range l.cfg.Headers {
		realValue, err := GetString(ev, v)
		if err != nil {
			log.Debug().Err(err).Msgf("parse template failed: %s", v)
			req.Header.Add(k, v)
		} else {
			log.Debug().Msgf("request header: {%s: %s}", k, realValue)
			req.Header.Add(k, realValue)
		}
	}

	resp, err := l.client.Do(req)
	if err != nil {
		return err
	}

	defer func() {
		err := resp.Body.Close()
		if err != nil {
			log.Error().Err(err).Msg("Failed to close response body")
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 300 || resp.StatusCode < 200 {
		return errors.New("not successful (2xx) response: " + string(body))
	}

	return nil
}

func (l *Loki) Close() {
	l.transport.CloseIdleConnections()
}
