package kube

import (
	"fmt"
	"testing"
	"time"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/metrics"
)

func getMetricsStore(t *testing.T) *metrics.Store {
	// generate a test-unique prefix using the test name + timestamp
	prefix := fmt.Sprintf("%s_%d_", t.Name(), time.Now().UnixNano())

	ms := metrics.NewMetricsStore(prefix)
	t.Cleanup(func() {
		metrics.DestroyMetricsStore(ms)
	})
	return ms
}

func TestEventWatcherRequiredValidate_MissingFields(t *testing.T) {
	tests := []struct {
		name    string
		setup   func() []EventWatcherOption
		wantErr bool
	}{
		{
			name: "OnEvent_nil",
			setup: func() []EventWatcherOption {
				return []EventWatcherOption{
					WithOnEventHandler(nil),
				}
			},
			wantErr: true,
		},
		{
			name: "MetricsStore_nil",
			setup: func() []EventWatcherOption {
				return []EventWatcherOption{
					WithMetricsStore(nil),
				}
			},
			wantErr: true,
		},
		{
			name: "OnEvent_and_MetricsStore_present",
			setup: func() []EventWatcherOption {
				return []EventWatcherOption{
					WithMetricsStore(getMetricsStore(t)),
					WithOnEventHandler(func(event *EnhancedEvent) {}),
				}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := tt.setup()
			_, err := NewEventWatcherRequired(opts...)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("%s: expected an error but got nil", tt.name)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", tt.name, err)
			}
		})
	}
}

func TestWatcherOptions_InvalidValues(t *testing.T) {

	tests := []struct {
		name string
		opts []EventWatcherOption
	}{
		{
			name: "MaxEventAgeSeconds_zero",
			opts: []EventWatcherOption{
				WithMaxEventAgeSeconds(0),
			},
		},
		{
			name: "CacheSize_zero",
			opts: []EventWatcherOption{
				WithCacheSize(0),
			},
		},
		{
			name: "MappingCacheSize_zero",
			opts: []EventWatcherOption{
				WithMappingCacheSize(0),
			},
		},
		{
			name: "CacheTTL_zero",
			opts: []EventWatcherOption{
				WithCacheTTL(0),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewEventWatcherRequired(tt.opts...)
			if err == nil {
				t.Fatalf("%s: expected an error but got nil", tt.name)
			}
		})
	}
}

func TestWatcherOptions_HappyPath(t *testing.T) {
	ms := getMetricsStore(t)

	opts := []EventWatcherOption{
		WithMetricsStore(ms),
		WithOnEventHandler(func(event *EnhancedEvent) {}),
		WithMaxEventAgeSeconds(120),
		WithCacheSize(256),
		WithMappingCacheSize(128),
		WithCacheTTL(5 * time.Minute),
		WithNamespace("default"),
		WithOmitLookup(false),
	}

	ewReq, err := NewEventWatcherRequired(opts...)
	if err != nil {
		t.Fatalf("unexpected error constructing EventWatcherRequired: %v", err)
	}

	if ewReq.metricsStore != ms {
		t.Fatalf("MetricsStore not preserved")
	}
	if ewReq.maxEventAgeSeconds != 120 {
		t.Fatalf("MaxEventAgeSeconds mismatch: got %d", ewReq.maxEventAgeSeconds)
	}
	if ewReq.cacheSize != 256 {
		t.Fatalf("CacheSize mismatch: got %d", ewReq.cacheSize)
	}
	if ewReq.mappingCacheSize != 128 {
		t.Fatalf("MappingCacheSize mismatch: got %d", ewReq.mappingCacheSize)
	}
	if ewReq.cacheTTL != 5*time.Minute {
		t.Fatalf("CacheTTL mismatch: got %s", ewReq.cacheTTL)
	}
	if ewReq.namespace != "default" {
		t.Fatalf("Namespace mismatch: got %s", ewReq.namespace)
	}
	if ewReq.omitLookup {
		t.Fatalf("OmitLookup mismatch: expected false")
	}
}
