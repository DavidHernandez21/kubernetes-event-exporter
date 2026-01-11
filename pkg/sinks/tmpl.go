package sinks

import (
	"bytes"
	"encoding/json"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"github.com/resmoio/kubernetes-event-exporter/pkg/kube"
)

func GetString(event *kube.EnhancedEvent, text string) (string, error) {
	tmpl, err := template.New("template").Funcs(sprig.TxtFuncMap()).Parse(text)
	if err != nil {
		return "", err
	}

	buf := new(bytes.Buffer)
	// TODO: Should we send event directly or more events?
	err = tmpl.Execute(buf, event)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func convertLayoutTemplate(layout map[string]any, ev *kube.EnhancedEvent) (map[string]any, error) {
	result := make(map[string]any, len(layout))

	for key, value := range layout {
		m, err := convertTemplate(value, ev)
		if err != nil {
			return nil, err
		}
		result[key] = m
	}
	return result, nil
}

func convertTemplate(value any, ev *kube.EnhancedEvent) (any, error) {
	switch v := value.(type) {
	case string:
		rendered, err := GetString(ev, v)
		if err != nil {
			return nil, err
		}

		return rendered, nil
	case map[any]any:
		strKeysMap := make(map[string]any)
		for k, v := range v {
			res, err := convertTemplate(v, ev)
			if err != nil {
				return nil, err
			}
			// TODO: It's a bit dangerous
			strKeysMap[k.(string)] = res
		}
		return strKeysMap, nil
	case map[string]any:
		strKeysMap := make(map[string]any)
		for k, v := range v {
			res, err := convertTemplate(v, ev)
			if err != nil {
				return nil, err
			}
			strKeysMap[k] = res
		}
		return strKeysMap, nil
	case []any:
		listConf := make([]any, len(v))
		for i := range v {
			t, err := convertTemplate(v[i], ev)
			if err != nil {
				return nil, err
			}
			listConf[i] = t
		}
		return listConf, nil
	}
	return nil, nil
}

func serializeEventWithLayout(layout map[string]any, ev *kube.EnhancedEvent) ([]byte, error) {
	var toSend []byte
	if layout != nil {
		res, err := convertLayoutTemplate(layout, ev)
		if err != nil {
			return nil, err
		}

		toSend, err = json.Marshal(res)
		if err != nil {
			return nil, err
		}
	} else {
		toSend = ev.ToJSON()
	}
	return toSend, nil
}
