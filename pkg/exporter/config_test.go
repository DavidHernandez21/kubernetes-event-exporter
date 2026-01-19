package exporter

import (
	"bytes"
	"os"
	"regexp"
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
	assert.Contains(t, output.String(), `"maxEventAgeSeconds":123`)
	assert.Contains(t, output.String(), "config.throttlePeriod is deprecated")
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
	assert.Contains(t, output.String(), `"maxEventAgeSeconds":123`)
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
	assert.Contains(t, output.String(), "cannot set both throttlePeriod (deprecated) and MaxEventAgeSeconds")
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

func TestValidate_FailsOnInvalidRegexPattern(t *testing.T) {
	const yml = `
route:
  drop:
    - apiVersion: "[invalid"
  match:
    - receiver: alert
      kind: "*(unclosed"
      labels:
        app: "++invalid"
receivers:
  - name: alert
    stdout: {}
`

	cfg := readConfig(t, yml)
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error parsing regexp")
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

func TestCompilePattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantNil bool
	}{
		{
			name:    "empty pattern returns nil",
			pattern: "",
			wantNil: true,
		},
		{
			name:    "valid simple pattern compiles",
			pattern: "Pod",
			wantNil: false,
		},
		{
			name:    "valid wildcard pattern compiles",
			pattern: ".*test.*",
			wantNil: false,
		},
		{
			name:    "valid alternation pattern compiles",
			pattern: "Pod|Deployment|ReplicaSet",
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compilePattern(tt.pattern)
			assert.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				// Verify it's a valid compiled regex
				assert.IsType(t, &regexp.Regexp{}, result)
			}
		})
	}
}

func TestCompilePatternMap(t *testing.T) {
	tests := []struct {
		name     string
		patterns map[string]string
		wantNil  bool
		wantLen  int
	}{
		{
			name:     "empty map returns nil",
			patterns: map[string]string{},
			wantNil:  true,
		},
		{
			name:     "nil map returns nil",
			patterns: nil,
			wantNil:  true,
		},
		{
			name: "single pattern compiles",
			patterns: map[string]string{
				"version": "dev",
			},
			wantNil: false,
			wantLen: 1,
		},
		{
			name: "multiple patterns compile",
			patterns: map[string]string{
				"version": "dev",
				"app":     "my-app",
				"env":     "prod|staging",
			},
			wantNil: false,
			wantLen: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compilePatternMap(tt.patterns)
			assert.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				assert.Len(t, result, tt.wantLen)
				// Verify all values are compiled regexes
				for k, v := range result {
					assert.NotNil(t, v, "pattern for key %s should not be nil", k)
					assert.IsType(t, &regexp.Regexp{}, v)
				}
			}
		})
	}
}

func TestPreCompilePatterns(t *testing.T) {
	const yml = `
route:
  drop:
    - namespace: "kube-system"
      type: "Normal"
  match:
    - receiver: stdout
      kind: "Pod|Deployment"
  routes:
    - drop:
        - minCount: 6
          apiVersion: "v.*"
      match:
        - receiver: alert
          namespace: ".*prod.*"
          labels:
            app: "critical-.*"
            env: "production"
          annotations:
            monitor: "true|enabled|1"
receivers:
  - name: stdout
    stdout: {}
  - name: alert
    stdout: {}
`

	cfg := readConfig(t, yml)

	// Precompile patterns
	err := cfg.PreCompilePatterns()
	assert.NoError(t, err)

	// Test top-level Drop rules
	assert.NotNil(t, cfg.Route.Drop[0].namespacePattern)
	assert.NotNil(t, cfg.Route.Drop[0].typePattern)
	assert.Nil(t, cfg.Route.Drop[0].kindPattern) // Not set, should be nil

	// Test top-level Match rules
	assert.NotNil(t, cfg.Route.Match[0].receiverPattern)
	assert.NotNil(t, cfg.Route.Match[0].kindPattern)

	// Test nested route Drop rules
	assert.NotNil(t, cfg.Route.Routes[0].Drop[0].apiVersionPattern)
	assert.Equal(t, int32(6), cfg.Route.Routes[0].Drop[0].MinCount)

	// Test nested route Match rules with labels and annotations
	assert.NotNil(t, cfg.Route.Routes[0].Match[0].receiverPattern)
	assert.NotNil(t, cfg.Route.Routes[0].Match[0].namespacePattern)
	assert.NotNil(t, cfg.Route.Routes[0].Match[0].labelsPatterns)
	assert.Len(t, cfg.Route.Routes[0].Match[0].labelsPatterns, 2)
	assert.NotNil(t, cfg.Route.Routes[0].Match[0].labelsPatterns["app"])
	assert.NotNil(t, cfg.Route.Routes[0].Match[0].labelsPatterns["env"])
	assert.NotNil(t, cfg.Route.Routes[0].Match[0].annotationsPatterns)
	assert.Len(t, cfg.Route.Routes[0].Match[0].annotationsPatterns, 1)
	assert.NotNil(t, cfg.Route.Routes[0].Match[0].annotationsPatterns["monitor"])

	// Verify patterns actually match correctly
	matched := cfg.Route.Match[0].kindPattern.MatchString("Pod")
	assert.True(t, matched)

	matched = cfg.Route.Match[0].kindPattern.MatchString("Deployment")
	assert.True(t, matched)

	matched = cfg.Route.Match[0].kindPattern.MatchString("Service")
	assert.False(t, matched)

	matched = cfg.Route.Routes[0].Match[0].labelsPatterns["app"].MatchString("critical-service")
	assert.True(t, matched)

	matched = cfg.Route.Routes[0].Match[0].labelsPatterns["env"].MatchString("production")
	assert.True(t, matched)

	matched = cfg.Route.Routes[0].Match[0].annotationsPatterns["monitor"].MatchString("enabled")
	assert.True(t, matched)

	matched = cfg.Route.Routes[0].Match[0].annotationsPatterns["monitor"].MatchString("1")
	assert.True(t, matched)

	matched = cfg.Route.Routes[0].Match[0].namespacePattern.MatchString("pre-prod-namespace")
	assert.True(t, matched)
}

func TestPreCompilePatterns_DeepNesting(t *testing.T) {
	const yml = `
route:
  routes:
    - match:
        - receiver: level1
          kind: "Pod"
      routes:
        - match:
            - receiver: level2
              namespace: "default"
          routes:
            - match:
                - receiver: level3
                  type: "Warning"
receivers:
  - name: level1
    stdout: {}
  - name: level2
    stdout: {}
  - name: level3
    stdout: {}
`

	cfg := readConfig(t, yml)
	err := cfg.PreCompilePatterns()
	assert.NoError(t, err)

	// Test level 1
	assert.NotNil(t, cfg.Route.Routes[0].Match[0].kindPattern)

	// Test level 2
	assert.NotNil(t, cfg.Route.Routes[0].Routes[0].Match[0].namespacePattern)

	// Test level 3
	assert.NotNil(t, cfg.Route.Routes[0].Routes[0].Routes[0].Match[0].typePattern)
}

func TestPreCompilePatterns_EmptyFields(t *testing.T) {
	const yml = `
route:
  match:
    - receiver: stdout
receivers:
  - name: stdout
    stdout: {}
`

	cfg := readConfig(t, yml)
	err := cfg.PreCompilePatterns()
	assert.NoError(t, err)

	rule := cfg.Route.Match[0]

	// Only receiver is set, all others should be nil
	assert.NotNil(t, rule.receiverPattern)
	assert.Nil(t, rule.kindPattern)
	assert.Nil(t, rule.namespacePattern)
	assert.Nil(t, rule.typePattern)
	assert.Nil(t, rule.messagePattern)
	assert.Nil(t, rule.labelsPatterns)
	assert.Nil(t, rule.annotationsPatterns)
}
