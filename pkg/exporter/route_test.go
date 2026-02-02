package exporter

import (
	"bytes"
	"slices"

	"testing"

	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/kube"
	"github.com/DavidHernandez21/kubernetes-event-exporter/pkg/sinks"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
)

// testReceiverRegistry just records the events to the registry so that tests can validate routing behavior
type testReceiverRegistry struct {
	rcvd map[string][]*kube.EnhancedEvent
}

func (t *testReceiverRegistry) Register(string, sinks.Sink) {
	panic("Why do you call this? It's for counting imaginary events for tests only")
}

func (t *testReceiverRegistry) SendEvent(name string, event *kube.EnhancedEvent) {
	if t.rcvd == nil {
		t.rcvd = make(map[string][]*kube.EnhancedEvent)
	}

	if _, ok := t.rcvd[name]; !ok {
		t.rcvd[name] = make([]*kube.EnhancedEvent, 0)
	}

	t.rcvd[name] = append(t.rcvd[name], event)
}

func (t *testReceiverRegistry) Close() {
	// No-op
}

func (t *testReceiverRegistry) isEventRcvd(name string, event *kube.EnhancedEvent) bool {
	if val, ok := t.rcvd[name]; !ok {
		return false
	} else {
		return slices.Contains(val, event)
	}
}

func (t *testReceiverRegistry) count(name string) int {
	if val, ok := t.rcvd[name]; ok {
		return len(val)
	} else {
		return 0
	}
}

func TestEmptyRoute(t *testing.T) {
	ev := kube.EnhancedEvent{}
	reg := testReceiverRegistry{}

	r := Route{}

	r.ProcessEvent(&ev, &reg)
	assert.Empty(t, reg.rcvd)
}

func TestBasicRoute(t *testing.T) {
	ev := kube.EnhancedEvent{}
	ev.Namespace = "kube-system"
	reg := testReceiverRegistry{}

	r := Route{
		Match: []Rule{{
			Namespace: "kube-system",
			Receiver:  "osman",
		}},
	}

	r.ProcessEvent(&ev, &reg)
	assert.True(t, reg.isEventRcvd("osman", &ev))
}

func TestDropRule(t *testing.T) {
	ev := kube.EnhancedEvent{}
	ev.Namespace = "kube-system"
	reg := testReceiverRegistry{}

	r := Route{
		Drop: []Rule{{
			Namespace: "kube-system",
		}},
		Match: []Rule{{
			Receiver: "osman",
		}},
	}

	r.ProcessEvent(&ev, &reg)
	assert.False(t, reg.isEventRcvd("osman", &ev))
	assert.Zero(t, reg.count("osman"))
}

func TestSingleLevelMultipleMatchRoute(t *testing.T) {
	ev := kube.EnhancedEvent{}
	ev.Namespace = "kube-system"
	reg := testReceiverRegistry{}

	r := Route{
		Match: []Rule{{
			Namespace: "kube-system",
			Receiver:  "osman",
		}, {
			Receiver: "any",
		}},
	}

	r.ProcessEvent(&ev, &reg)
	assert.True(t, reg.isEventRcvd("osman", &ev))
	assert.True(t, reg.isEventRcvd("any", &ev))
}

func TestSubRoute(t *testing.T) {
	ev := kube.EnhancedEvent{}
	ev.Namespace = "kube-system"
	reg := testReceiverRegistry{}

	r := Route{
		Match: []Rule{{
			Namespace: "kube-system",
		}},
		Routes: []Route{{
			Match: []Rule{{
				Receiver: "osman",
			}},
		}},
	}

	r.ProcessEvent(&ev, &reg)

	assert.True(t, reg.isEventRcvd("osman", &ev))
}

func TestSubSubRoute(t *testing.T) {
	ev := kube.EnhancedEvent{}
	ev.Namespace = "kube-system"
	reg := testReceiverRegistry{}

	r := Route{
		Match: []Rule{{
			Namespace: "kube-*",
		}},
		Routes: []Route{{
			Match: []Rule{{
				Receiver: "osman",
			}},
			Routes: []Route{{
				Match: []Rule{{
					Receiver: "any",
				}},
			}},
		}},
	}

	r.ProcessEvent(&ev, &reg)

	assert.True(t, reg.isEventRcvd("osman", &ev))
	assert.True(t, reg.isEventRcvd("any", &ev))
}

func TestSubSubRouteWithDrop(t *testing.T) {
	ev := kube.EnhancedEvent{}
	ev.Namespace = "kube-system"
	reg := testReceiverRegistry{}

	r := Route{
		Match: []Rule{{
			Namespace: "kube-*",
		}},
		Routes: []Route{{
			Match: []Rule{{
				Receiver: "osman",
			}},
			Routes: []Route{{
				Drop: []Rule{{
					Namespace: "kube-system",
				}},
				Match: []Rule{{
					Receiver: "any",
				}},
			}},
		}},
	}

	r.ProcessEvent(&ev, &reg)

	assert.True(t, reg.isEventRcvd("osman", &ev))
	assert.False(t, reg.isEventRcvd("any", &ev))
}

// Test for issue: https://github.com/DavidHernandez21/kubernetes-event-exporter/issues/51
func Test_GHIssue51(t *testing.T) {
	ev1 := kube.EnhancedEvent{}
	ev1.Type = "Warning"
	ev1.Reason = "FailedCreatePodContainer"

	ev2 := kube.EnhancedEvent{}
	ev2.Type = "Warning"
	ev2.Reason = "FailedCreate"

	reg := testReceiverRegistry{}

	r := Route{
		Drop: []Rule{{
			Type: "Normal",
		}},
		Match: []Rule{{
			Reason:   "FailedCreatePodContainer",
			Receiver: "elastic",
		}},
	}

	r.ProcessEvent(&ev1, &reg)
	r.ProcessEvent(&ev2, &reg)

	assert.True(t, reg.isEventRcvd("elastic", &ev1))
	assert.False(t, reg.isEventRcvd("elastic", &ev2))
}

// mustCompileRule is a helper to compile rule patterns for tests
func mustCompileRule(t testing.TB, rule Rule) Rule {
	cfg := Config{Route: Route{Match: []Rule{rule}}}
	if err := cfg.PreCompilePatterns(); err != nil {
		t.Fatalf("failed to compile rule patterns: %v", err)
	}
	return cfg.Route.Match[0]
}

func TestBasicRoutePattern(t *testing.T) {
	ev := kube.EnhancedEvent{}
	ev.Namespace = "kube-system"
	reg := testReceiverRegistry{}

	r := Route{
		Match: []Rule{mustCompileRule(t, Rule{
			Namespace: "kube-sys.+",
			Receiver:  "osman",
		})},
	}

	// check that precompiled patterns work as expected
	assert.NotNil(t, r.Match[0].namespacePattern)
	assert.NotNil(t, r.Match[0].receiverPattern)

	r.ProcessEvent(&ev, &reg)
	assert.True(t, reg.isEventRcvd("osman", &ev))

	output := &bytes.Buffer{}
	log.Logger = log.Logger.Output(output)
	assert.NotContains(t, output.String(), "falling back to runtime compilation")

}

func BenchmarkMatchesEvent_WithPrecompile(b *testing.B) {
	ev := kube.EnhancedEvent{}
	ev.Namespace = "kube-system"

	rule := mustCompileRule(b, Rule{
		Namespace: "kube-.*",
	})

	for b.Loop() {
		rule.MatchesEvent(&ev)
	}
}

func BenchmarkMatchesEvent_WithoutPrecompile(b *testing.B) {
	ev := kube.EnhancedEvent{}
	ev.Namespace = "kube-system"

	rule := Rule{
		Namespace: "kube-.*",
	}

	for b.Loop() {
		rule.MatchesEvent(&ev)
	}
}
