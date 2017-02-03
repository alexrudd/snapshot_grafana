package snapshot

import (
	"net/url"
	"reflect"
	"testing"
	"time"
)

func TestProcessConfig(t *testing.T) {
	// some vars
	urlGraf, _ := url.Parse("https://grafana.net/")
	urlRain, _ := url.Parse("https://snapshot.raintank.io/")
	urlGrafSuff, _ := url.Parse("https://grafana.net")
	urlRainSuff, _ := url.Parse("https://snapshot.raintank.io")
	// configs to test
	var configTests = []struct {
		purpose  string
		in       *Config // input config
		expected *Config // expected config
		valid    bool    // expected result
	}{
		{
			purpose: "Basic valid Config",
			in: &Config{
				GrafanaAddr:   urlGraf,
				GrafanaAPIKey: "XXXXX",
			},
			expected: &Config{
				GrafanaAddr:    urlGraf,
				GrafanaAPIKey:  "XXXXX",
				SnapshotAddr:   urlGraf,
				SnapshotAPIKey: "XXXXX",
			},
			valid: true,
		},
		{
			purpose: "Complete valid Config",
			in: &Config{
				GrafanaAddr:    urlGraf,
				GrafanaAPIKey:  "YYYYY",
				SnapshotAddr:   urlRain,
				SnapshotAPIKey: "ZZZZZ",
			},
			expected: &Config{
				GrafanaAddr:    urlGraf,
				GrafanaAPIKey:  "YYYYY",
				SnapshotAddr:   urlRain,
				SnapshotAPIKey: "ZZZZZ",
			},
			valid: true,
		},
		{
			purpose: "Complete valid Config URL suffix check",
			in: &Config{
				GrafanaAddr:    urlGrafSuff,
				GrafanaAPIKey:  "YYYYY",
				SnapshotAddr:   urlRainSuff,
				SnapshotAPIKey: "ZZZZZ",
			},
			expected: &Config{
				GrafanaAddr:    urlGraf,
				GrafanaAPIKey:  "YYYYY",
				SnapshotAddr:   urlRain,
				SnapshotAPIKey: "ZZZZZ",
			},
			valid: true,
		},
		{
			purpose: "Missing required GrafanaAPIKey",
			in: &Config{
				GrafanaAddr: urlGraf,
			},
			expected: nil,
			valid:    false,
		},
		{
			purpose: "Missing required GrafanaAddr",
			in: &Config{
				GrafanaAPIKey: "XXXXX",
			},
			expected: nil,
			valid:    false,
		},
		{
			purpose: "Missing required both fields",
			in: &Config{
				SnapshotAddr:   urlRain,
				SnapshotAPIKey: "ZZZZZ",
			},
			expected: nil,
			valid:    false,
		},
	}
	// test
	for _, ct := range configTests {
		out, err := processConfig(ct.in)
		if ct.valid && err != nil {
			t.Errorf("Test \"%s\" unexpectedly failed validation: %s", ct.purpose, err.Error())
		} else if !ct.valid && err == nil {
			t.Errorf("Test \"%s\" unexpectedly passed validation", ct.purpose)
		} else {
			if !reflect.DeepEqual(out, ct.expected) {
				t.Errorf("Test \"%s\" DeepEqual compare failed", ct.purpose)
				t.Logf("Expected:\n%v\nActual:\n%v", ct.expected, out)
			}
		}
	}
}

func TestProcessTakeConfig(t *testing.T) {
	// some vars
	from := time.Date(2017, time.February, 05, 6, 0, 0, 0, time.Local)
	to := time.Date(2017, time.February, 05, 12, 0, 0, 0, time.Local)
	vars := make(map[string]string)
	vars["key1"] = "val1"
	vars["key2"] = "val2"
	// configs to test
	var takeConfigTests = []struct {
		purpose  string
		in       *TakeConfig // input config
		expected *TakeConfig // expected config
		valid    bool        // expected result
	}{
		{
			purpose: "Simple valid config",
			in: &TakeConfig{
				DashSlug: "test-slug",
				From:     &from,
				To:       &to,
			},
			expected: &TakeConfig{
				DashSlug:     "test-slug",
				From:         &from,
				To:           &to,
				Vars:         make(map[string]string),
				Expires:      time.Second * 0,
				SnapshotName: from.Format("2006-01-02") + " test-slug",
			},
			valid: true,
		},
		{
			purpose: "Complete valid config",
			in: &TakeConfig{
				DashSlug:     "test-slug",
				From:         &from,
				To:           &to,
				Vars:         vars,
				Expires:      time.Second * 3600,
				SnapshotName: "My Test Snapshot",
			},
			expected: &TakeConfig{
				DashSlug:     "test-slug",
				From:         &from,
				To:           &to,
				Vars:         vars,
				Expires:      time.Second * 3600,
				SnapshotName: "My Test Snapshot",
			},
			valid: true,
		},
		{
			purpose: "Invalid time-range",
			in: &TakeConfig{
				DashSlug: "test-slug",
				From:     &to,
				To:       &from,
			},
			expected: nil,
			valid:    false,
		},
		{
			purpose: "Missing required fields",
			in: &TakeConfig{
				Vars:         vars,
				Expires:      time.Second * 3600,
				SnapshotName: "My Test Snapshot",
			},
			expected: nil,
			valid:    false,
		},
	}
	// test
	for _, tct := range takeConfigTests {
		out, err := processTakeConfig(tct.in)
		if tct.valid && err != nil {
			t.Errorf("Test \"%s\" unexpectedly failed validation: %s", tct.purpose, err.Error())
		} else if !tct.valid && err == nil {
			t.Errorf("Test \"%s\" unexpectedly passed validation", tct.purpose)
		} else {
			if !reflect.DeepEqual(out, tct.expected) {
				t.Errorf("Test \"%s\" DeepEqual compare failed", tct.purpose)
				t.Logf("Expected:\n%v\nActual:\n%v", tct.expected, out)
			}
		}
	}
}
