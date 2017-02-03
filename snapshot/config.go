package snapshot

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Config for configuring the snapshot pacakge with the addresses and
// API keys for the grafana host and snapshot host. API key for require Grafana
// require admin privelages; the API for the snapshot host require at least
// editor privelages.
type Config struct {
	GrafanaAPIKey  string
	SnapshotAPIKey string
	GrafanaAddr    *url.URL
	SnapshotAddr   *url.URL
}

// TakeConfig for defining exactly which dashboard and time-range to snapshot,
// and also the name and expiry duration of the snapshot.
type TakeConfig struct {
	DashSlug     string
	From         *time.Time
	To           *time.Time
	Vars         map[string]string
	Expires      time.Duration
	SnapshotName string
}

func processConfig(configIn *Config) (*Config, error) {
	configOut := &Config{}

	// Grafana Address
	if configIn.GrafanaAddr == nil || len(configIn.GrafanaAddr.String()) == 0 {
		return nil, errors.New("Missing required Config field: \"GrafanaAddr\"")
	}
	configOut.GrafanaAddr = configIn.GrafanaAddr

	if !strings.HasSuffix(configOut.GrafanaAddr.Path, "/") {
		configOut.GrafanaAddr.Path = configOut.GrafanaAddr.Path + "/"
	}

	// Grafana API key
	if len(configIn.GrafanaAPIKey) == 0 {
		return nil, errors.New("Missing required Config field: \"GrafanaAPIKey\"")
	}
	configOut.GrafanaAPIKey = configIn.GrafanaAPIKey

	// Parse Snapshot host Address or default to Grafana address
	if configIn.SnapshotAddr == nil || len(configIn.SnapshotAddr.String()) == 0 {
		configOut.SnapshotAddr = configIn.GrafanaAddr
	} else {
		configOut.SnapshotAddr = configIn.SnapshotAddr
	}
	if !strings.HasSuffix(configOut.SnapshotAddr.Path, "/") {
		configOut.SnapshotAddr.Path = configOut.SnapshotAddr.Path + "/"
	}

	// Snapshot API key
	if len(configIn.SnapshotAPIKey) == 0 {
		configOut.SnapshotAPIKey = configIn.GrafanaAPIKey
	} else {
		configOut.SnapshotAPIKey = configIn.SnapshotAPIKey
	}

	// return ok
	return configOut, nil
}

func processTakeConfig(configIn *TakeConfig) (*TakeConfig, error) {
	configOut := &TakeConfig{}

	// Parse DashSlug
	if len(configIn.DashSlug) == 0 {
		return nil, errors.New("Missing required Config field: \"DashSlug\"")
	}
	configOut.DashSlug = configIn.DashSlug

	// Parse From
	if configIn.From == nil {
		return nil, errors.New("Missing required Config field: \"From\"")
	}
	configOut.From = configIn.From

	// Parse To
	if configIn.To == nil {
		return nil, errors.New("Missing required Config field: \"To\"")
	} else if !configIn.From.Before(*configIn.To) {
		return nil, errors.New("TakeConfig \"To\" field must come cronologically after \"From\"")
	}
	configOut.To = configIn.To

	// Parse Vars
	if configIn.Vars == nil {
		configOut.Vars = make(map[string]string)
	} else {
		configOut.Vars = configIn.Vars
	}
	// Parse Expires
	if configIn.Expires < 0 {
		configOut.Expires = 0
	} else {
		configOut.Expires = configIn.Expires
	}
	// Parse SnapshotName
	if len(configIn.SnapshotName) == 0 {
		configOut.SnapshotName = fmt.Sprintf("%s %s", configIn.To.Format("2006-01-02"), configIn.DashSlug)
	} else {
		configOut.SnapshotName = configIn.SnapshotName
	}

	// return ok
	return configOut, nil
}
