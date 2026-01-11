package exporter

import (
	"bytes"
	"os"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func readConfig(t *testing.T, yml string) Config {
	var cfg Config
	err := yaml.Unmarshal([]byte(yml), &cfg)
	if err != nil {
		t.Fatal("Cannot parse yaml", err)
	}
	return cfg
}

func Test_ParseConfig(t *testing.T) {
	const yml = `
route:
  routes:
    - drop:
        - minCount: 6
          apiVersion: v33
      match:
        - receiver: stdout
receivers:
  - name: stdout
    stdout: {}
`

	cfg := readConfig(t, yml)

	assert.Len(t, cfg.Route.Routes, 1)
	assert.Len(t, cfg.Route.Routes[0].Drop, 1)
	assert.Len(t, cfg.Route.Routes[0].Match, 1)
	assert.Len(t, cfg.Route.Routes[0].Drop, 1)

	assert.Equal(t, int32(6), cfg.Route.Routes[0].Drop[0].MinCount)
	assert.Equal(t, "v33", cfg.Route.Routes[0].Drop[0].APIVersion)
	assert.Equal(t, "stdout", cfg.Route.Routes[0].Match[0].Receiver)
}

func TestValidate_IsCheckingMaxEventAgeSeconds_WhenNotSet(t *testing.T) {
	config := Config{}
	err := config.Validate()
	assert.True(t, config.MaxEventAgeSeconds == 5)
	assert.NoError(t, err)
}

func TestValidate_IsCheckingMaxEventAgeSeconds_WhenThrottledPeriodSet(t *testing.T) {
	output := &bytes.Buffer{}
	log.Logger = log.Logger.Output(output)

	config := Config{
		ThrottlePeriod: 123,
	}
	err := config.Validate()

	assert.True(t, config.MaxEventAgeSeconds == 123)
	assert.Contains(t, output.String(), "config.maxEventAgeSeconds=123")
	assert.Contains(t, output.String(), "config.throttlePeriod is depricated, consider using config.maxEventAgeSeconds instead")
	assert.NoError(t, err)
}

func TestValidate_IsCheckingMaxEventAgeSeconds_WhenMaxEventAgeSecondsSet(t *testing.T) {
	output := &bytes.Buffer{}
	log.Logger = log.Logger.Output(output)

	config := Config{
		MaxEventAgeSeconds: 123,
	}
	err := config.Validate()
	assert.True(t, config.MaxEventAgeSeconds == 123)
	assert.Contains(t, output.String(), "config.maxEventAgeSeconds=123")
	assert.NoError(t, err)
}

func TestValidate_IsCheckingMaxEventAgeSeconds_WhenMaxEventAgeSecondsAndThrottledPeriodSet(t *testing.T) {
	output := &bytes.Buffer{}
	log.Logger = log.Logger.Output(output)

	config := Config{
		ThrottlePeriod:     123,
		MaxEventAgeSeconds: 321,
	}
	err := config.Validate()
	assert.Error(t, err)
	assert.Contains(t, output.String(), "cannot set both throttlePeriod (depricated) and MaxEventAgeSeconds")
}

func TestValidate_MetricsNamePrefix_WhenEmpty(t *testing.T) {
	output := &bytes.Buffer{}
	log.Logger = log.Logger.Output(output)

	config := Config{}
	err := config.Validate()
	assert.NoError(t, err)
	assert.Equal(t, "", config.MetricsNamePrefix)
	assert.Contains(t, output.String(), "metrics name prefix is empty, setting config.metricsNamePrefix='event_exporter_' is recommended")
}

func TestValidate_MetricsNamePrefix_WhenValid(t *testing.T) {
	output := &bytes.Buffer{}
	log.Logger = log.Logger.Output(output)

	validCases := []string{
		"kubernetes_event_exporter_",
		"test_",
		"test_test_",
		"test::test_test_",
		"TEST::test_test_",
		"test_test::1234_test_",
	}

	for _, testPrefix := range validCases {
		config := Config{
			MetricsNamePrefix: testPrefix,
		}
		err := config.Validate()
		assert.NoError(t, err)
		assert.Equal(t, testPrefix, config.MetricsNamePrefix)
		assert.Contains(t, output.String(), "config.metricsNamePrefix='"+testPrefix+"'")
	}
}

func TestValidate_MetricsNamePrefix_WhenInvalid(t *testing.T) {
	output := &bytes.Buffer{}
	log.Logger = log.Logger.Output(output)

	invalidCases := []string{
		"no_tracing_underscore",
		"__reserved_",
		"::wrong_",
		"13245_test_",
	}

	for _, testPrefix := range invalidCases {
		config := Config{
			MetricsNamePrefix: testPrefix,
		}
		err := config.Validate()
		assert.Error(t, err)
		assert.Equal(t, testPrefix, config.MetricsNamePrefix)
		assert.Contains(t, output.String(), "config.metricsNamePrefix should match the regex: ^[a-zA-Z][a-zA-Z0-9_:]*_$")
	}
}

func TestSetDefaults(t *testing.T) {
	config := Config{}
	config.SetDefaults()
	require.Equal(t, DefaultCacheSize, config.CacheSize)
	require.Equal(t, rest.DefaultQPS, config.KubeQPS)
	require.Equal(t, rest.DefaultBurst, config.KubeBurst)
}

func TestSetDefaults_MappingCacheSizeEnv(t *testing.T) {
	tests := []struct {
		name              string
		cfg               Config
		envValue          *string
		wantSize          int
		wantLogSubstrings []string
	}{
		{
			name:     "no config and no env uses default",
			cfg:      Config{},
			envValue: nil,
			wantSize: DefaultMappingCacheSize,
			wantLogSubstrings: []string{
				"no mappingCacheSizeOverride set; using max of 1/4 cacheSize or",
			},
		},
		{
			name:     "no env and config use too small cache value uses default",
			cfg:      Config{CacheSize: 10},
			envValue: nil,
			wantSize: DefaultMappingCacheSize,
			wantLogSubstrings: []string{
				"no mappingCacheSizeOverride set; using max of 1/4 cacheSize or",
			},
		},
		{
			name:     "no env and config use larger cache value uses quarter of cache size",
			cfg:      Config{CacheSize: 2048},
			envValue: nil,
			wantSize: 512,
			wantLogSubstrings: []string{
				"no mappingCacheSizeOverride set; using max of 1/4 cacheSize or",
			},
		},
		{
			name: "config value takes precedence over env",
			cfg: Config{
				MappingCacheSize: 128,
			},
			envValue: func() *string { v := "256"; return &v }(),
			wantSize: 128,
			wantLogSubstrings: []string{
				"setting config.mappingCacheSize from config",
			},
		},
		{
			name:     "valid env value is used",
			cfg:      Config{},
			envValue: func() *string { v := "256"; return &v }(),
			wantSize: 256,
			wantLogSubstrings: []string{
				"using MAPPING_CACHE_SIZE from environment",
			},
		},
		{
			name:     "invalid non-numeric env leaves size at zero and logs warning",
			cfg:      Config{},
			envValue: func() *string { v := "not-a-number"; return &v }(),
			wantSize: 0,
			wantLogSubstrings: []string{
				"invalid MAPPING_CACHE_SIZE value; expected positive integer",
			},
		},
		{
			name:     "zero env value leaves size at zero and logs warning",
			cfg:      Config{},
			envValue: func() *string { v := "0"; return &v }(),
			wantSize: 0,
			wantLogSubstrings: []string{
				"invalid MAPPING_CACHE_SIZE value; expected positive integer",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Ensure env is clean, then optionally set it.
			os.Unsetenv("MAPPING_CACHE_SIZE")
			if tt.envValue != nil {
				t.Setenv("MAPPING_CACHE_SIZE", *tt.envValue)
			}

			output := &bytes.Buffer{}
			log.Logger = log.Logger.Output(output)

			config := tt.cfg
			config.SetDefaults()

			require.Equal(t, tt.wantSize, config.MappingCacheSize)
			for _, substr := range tt.wantLogSubstrings {
				assert.Contains(t, output.String(), substr)
			}
		})
	}
}
