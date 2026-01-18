package exporter

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"

	"github.com/resmoio/kubernetes-event-exporter/pkg/kube"
	"github.com/resmoio/kubernetes-event-exporter/pkg/sinks"
	"github.com/rs/zerolog/log"
	"k8s.io/client-go/rest"
)

const (
	DefaultCacheSize        = 1024
	DefaultMappingCacheSize = DefaultCacheSize / 4
)

// Config allows configuration
type Config struct {
	LeaderElection kube.LeaderElectionConfig `yaml:"leaderElection"`
	// Route is the top route that the events will match
	// TODO: There is currently a tight coupling with route and config, but not with receiver config and sink so
	// TODO: I am not sure what to do here.
	LogLevel           string                 `yaml:"logLevel"`
	LogFormat          string                 `yaml:"logFormat"`
	ClusterName        string                 `yaml:"clusterName,omitempty"`
	Namespace          string                 `yaml:"namespace"`
	MetricsNamePrefix  string                 `yaml:"metricsNamePrefix,omitempty"`
	Route              Route                  `yaml:"route"`
	Receivers          []sinks.ReceiverConfig `yaml:"receivers"`
	ThrottlePeriod     int64                  `yaml:"throttlePeriod"`
	MaxEventAgeSeconds int64                  `yaml:"maxEventAgeSeconds"`
	KubeBurst          int                    `yaml:"kubeBurst,omitempty"`
	CacheSize          int                    `yaml:"cacheSize,omitempty"`
	MappingCacheSize   int                    `yaml:"mappingCacheSize,omitempty"`
	KubeQPS            float32                `yaml:"kubeQPS,omitempty"`
	OmitLookup         bool                   `yaml:"omitLookup,omitempty"`
}

func (c *Config) SetDefaults() {
	if c.CacheSize == 0 {
		c.CacheSize = DefaultCacheSize
		log.Debug().Msg("setting config.cacheSize=1024 (default)")
	}

	if c.MappingCacheSize > 0 {
		log.Debug().Int("mappingCacheSize", c.MappingCacheSize).Msg("setting config.mappingCacheSize from config")
	} else {
		// Fallback to environment variable if set
		if v, ok := os.LookupEnv("MAPPING_CACHE_SIZE"); ok {
			if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
				c.MappingCacheSize = parsed
				log.Debug().Int("mappingCacheSizeOverride", parsed).Msg("using MAPPING_CACHE_SIZE from environment")
			} else {
				log.Warn().Str("MAPPING_CACHE_SIZE", v).Msg("invalid MAPPING_CACHE_SIZE value; expected positive integer")
			}
		} else {
			log.Debug().Msg("no mappingCacheSizeOverride set; using max of 1/4 cacheSize or 1024/4 (default)")
			c.MappingCacheSize = max(DefaultMappingCacheSize, c.CacheSize/4)
		}

	}

	if c.KubeBurst == 0 {
		c.KubeBurst = rest.DefaultBurst
		log.Debug().Msg(fmt.Sprintf("setting config.kubeBurst=%d (default)", rest.DefaultBurst))
	}

	if c.KubeQPS == 0 {
		c.KubeQPS = rest.DefaultQPS
		log.Debug().Msg(fmt.Sprintf("setting config.kubeQPS=%.2f (default)", rest.DefaultQPS))
	}
}

func (c *Config) Validate() error {
	if err := c.validateDefaults(); err != nil {
		return err
	}
	if err := c.validateMetricsNamePrefix(); err != nil {
		return err
	}

	// Precompile all regex patterns
	err := c.PreCompilePatterns()
	if err != nil {
		return err
	}

	// No duplicate receivers
	// Receivers individually
	// Routers recursive
	return nil
}

func (c *Config) validateDefaults() error {
	if err := c.validateMaxEventAgeSeconds(); err != nil {
		return err
	}
	return nil
}

func (c *Config) validateMaxEventAgeSeconds() error {
	// If both are set, that's an error.
	if c.ThrottlePeriod != 0 && c.MaxEventAgeSeconds != 0 {
		log.Error().Msg("cannot set both throttlePeriod (deprecated) and MaxEventAgeSeconds")
		return errors.New("validateMaxEventAgeSeconds failed")
	}

	// If throttlePeriod is set, use it but warn it's deprecated.
	if c.ThrottlePeriod != 0 {
		c.MaxEventAgeSeconds = c.ThrottlePeriod
		log.Warn().Msg("config.throttlePeriod is deprecated, consider using config.maxEventAgeSeconds instead")
	}

	// If still zero, set default.
	if c.MaxEventAgeSeconds == 0 {
		c.MaxEventAgeSeconds = 5
		log.Info().Int64("maxEventAgeSeconds", c.MaxEventAgeSeconds).Msg("setting config.maxEventAgeSeconds to default")
		return nil
	}

	// Final log of the effective value.
	log.Info().Int64("maxEventAgeSeconds", c.MaxEventAgeSeconds).Msg("config.maxEventAgeSeconds")
	return nil
}

func (c *Config) validateMetricsNamePrefix() error {
	if c.MetricsNamePrefix != "" {
		// https://prometheus.io/docs/concepts/data_model/#metric-names-and-labels
		checkResult, err := regexp.MatchString("^[a-zA-Z][a-zA-Z0-9_:]*_$", c.MetricsNamePrefix)
		if err != nil {
			return err
		}
		if checkResult {
			log.Info().Msg("config.metricsNamePrefix='" + c.MetricsNamePrefix + "'")
		} else {
			log.Error().Msg("config.metricsNamePrefix should match the regex: ^[a-zA-Z][a-zA-Z0-9_:]*_$")
			return errors.New("validateMetricsNamePrefix failed")
		}
	} else {
		log.Warn().Msg("metrics name prefix is empty, setting config.metricsNamePrefix='event_exporter_' is recommended")
	}
	return nil
}

// compilePattern compiles a regex pattern if it's not empty, returns nil otherwise
func compilePattern(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, nil
	}
	return regexp.Compile(pattern)
}

// compilePatternMap compiles all regex patterns in a map
func compilePatternMap(patterns map[string]string) (map[string]*regexp.Regexp, error) {
	if len(patterns) == 0 {
		return nil, nil
	}

	compiled := make(map[string]*regexp.Regexp, len(patterns))
	for k, v := range patterns {
		re, err := compilePattern(v)
		if err != nil {
			return nil, fmt.Errorf("invalid regex pattern for key '%s': %w", k, err)
		}
		compiled[k] = re
	}
	return compiled, nil
}

// preCompilePatternsHelper precompiles regex patterns for a given rule
func (c *Config) preCompilePatternsHelper(rule *Rule) error {
	var err error
	rule.aPIVersionPattern, err = compilePattern(rule.APIVersion)
	if err != nil {
		return err
	}

	rule.kindPattern, err = compilePattern(rule.Kind)
	if err != nil {
		return err
	}
	rule.namespacePattern, err = compilePattern(rule.Namespace)
	if err != nil {
		return err
	}
	rule.reasonPattern, err = compilePattern(rule.Reason)
	if err != nil {
		return err
	}
	rule.typePattern, err = compilePattern(rule.Type)
	if err != nil {
		return err
	}
	rule.componentPattern, err = compilePattern(rule.Component)
	if err != nil {
		return err
	}
	rule.hostPattern, err = compilePattern(rule.Host)
	if err != nil {
		return err
	}
	rule.messagePattern, err = compilePattern(rule.Message)
	if err != nil {
		return err
	}
	rule.receiverPattern, err = compilePattern(rule.Receiver)
	if err != nil {
		return err
	}
	rule.labelsPatterns, err = compilePatternMap(rule.Labels)
	if err != nil {
		return err
	}
	rule.annotationsPatterns, err = compilePatternMap(rule.Annotations)
	if err != nil {
		return err
	}
	return nil
}

// preCompileRoute precompiles regex patterns for all rules in a route, including nested routes
func (c *Config) preCompileRoute(route *Route) error {
	for i := range route.Drop {
		if err := c.preCompilePatternsHelper(&route.Drop[i]); err != nil {
			return err
		}
	}

	for i := range route.Match {
		if err := c.preCompilePatternsHelper(&route.Match[i]); err != nil {
			return err
		}
	}

	// Recursively compile patterns for nested Routes
	for i := range route.Routes {
		if err := c.preCompileRoute(&route.Routes[i]); err != nil {
			return err
		}
	}

	return nil
}

func (c *Config) PreCompilePatterns() error {
	return c.preCompileRoute(&c.Route)
}
