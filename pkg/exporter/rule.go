package exporter

import (
	"regexp"

	"github.com/rs/zerolog/log"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
)

// matchString is a method to clean the code. Error handling is omitted here because these
// rules are validated before use. According to regexp.MatchString, the only way it fails its
// that the pattern does not compile.
//
//nolint:errcheck
func matchString(pattern, s string) bool {
	matched, _ := regexp.MatchString(pattern, s)
	return matched
}

// Rule is for matching an event
type Rule struct {
	Labels      map[string]string
	Annotations map[string]string

	// Precompiled patterns. Populated when the rule is created.
	labelsPatterns      map[string]*regexp.Regexp
	annotationsPatterns map[string]*regexp.Regexp
	apiVersionPattern   *regexp.Regexp
	kindPattern         *regexp.Regexp
	namespacePattern    *regexp.Regexp
	reasonPattern       *regexp.Regexp
	typePattern         *regexp.Regexp
	componentPattern    *regexp.Regexp
	hostPattern         *regexp.Regexp
	messagePattern      *regexp.Regexp
	receiverPattern     *regexp.Regexp

	// Fields to match against
	Message    string
	APIVersion string `yaml:"apiVersion"`
	Kind       string
	Namespace  string
	Reason     string
	Type       string
	Component  string
	Host       string
	Receiver   string
	MinCount   int32 `yaml:"minCount"`
}

type fieldMatcher struct {
	pattern   *regexp.Regexp
	ruleName  string
	eventName string
}

// MatchesEvent compares the rule to an event and returns a boolean value to indicate
// whether the event is compatible with the rule. All fields are compared as regular expressions
// so the user must keep that in mind while writing rules.
//
// Note: In production, patterns should be precompiled via PreCompilePatterns() during validation.
// This method falls back to runtime compilation for testing and backward compatibility.
//
//nolint:gocyclo
func (r *Rule) MatchesEvent(ev *kube.EnhancedEvent) bool {
	// These matchers are just basic comparison matchers, if one of them fails, it means the event does not match the rule
	matchers := []fieldMatcher{
		{pattern: r.messagePattern, ruleName: r.Message, eventName: ev.Message},
		{pattern: r.apiVersionPattern, ruleName: r.APIVersion, eventName: ev.InvolvedObject.APIVersion},
		{pattern: r.kindPattern, ruleName: r.Kind, eventName: ev.InvolvedObject.Kind},
		{pattern: r.namespacePattern, ruleName: r.Namespace, eventName: ev.Namespace},
		{pattern: r.reasonPattern, ruleName: r.Reason, eventName: ev.Reason},
		{pattern: r.typePattern, ruleName: r.Type, eventName: ev.Type},
		{pattern: r.componentPattern, ruleName: r.Component, eventName: ev.Source.Component},
		{pattern: r.hostPattern, ruleName: r.Host, eventName: ev.Source.Host},
	}

	for _, m := range matchers {
		if m.ruleName == "" {
			continue
		}

		if m.pattern != nil {
			if !m.pattern.MatchString(m.eventName) {
				return false
			}
		} else {
			log.Debug().Msgf("Rule field '%s' is not precompiled, falling back to runtime compilation", m.ruleName)
			if !matchString(m.ruleName, m.eventName) {
				return false
			}
		}
	}

	// Labels are also mutually exclusive, they all need to be present
	if len(r.Labels) > 0 {
		for k, v := range r.Labels {
			val, ok := ev.InvolvedObject.Labels[k]
			if !ok {
				return false
			}

			if pattern := r.labelsPatterns[k]; pattern != nil {
				if !pattern.MatchString(val) {
					return false
				}
			} else {
				log.Debug().Msgf("Rule label '%s' is not precompiled, falling back to runtime compilation", k)
				if !matchString(v, val) {
					return false
				}
			}
		}
	}

	// Annotations are also mutually exclusive, they all need to be present
	if len(r.Annotations) > 0 {
		for k, v := range r.Annotations {
			val, ok := ev.InvolvedObject.Annotations[k]
			if !ok {
				return false
			}

			if pattern := r.annotationsPatterns[k]; pattern != nil {
				if !pattern.MatchString(val) {
					return false
				}
			} else {
				log.Debug().Msgf("Rule annotation '%s' is not precompiled, falling back to runtime compilation", k)
				if !matchString(v, val) {
					return false
				}
			}
		}
	}

	// If minCount is not given via a config, it's already 0 and the count is already 1 and this passes.
	if ev.Count < r.MinCount {
		return false
	}

	// If it failed every step, it must match because our matchers are limiting
	return true
}
