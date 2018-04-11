package main

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
	"github.com/alexrudd/snapshot_grafana/snapshot"
)

var (
	timeLayout      = "2006-01-02 15:04:05"
	grafanaAddr     = flag.String("grafana_addr", "http://localhost:3000/", "The address of the Grafana instance to snapshot.")
	grafanaAPIKey   = flag.String("grafana_api_key", "", "The address of the Grafana instance to snapshot.")
	snapshotAddr    = flag.String("snapshot_addr", "", "The location to submit the snapshot. Defaults to the grafana address.")
	snapshotAPIKey  = flag.String("snapshot_api_key", "", "The address of the Grafana instance to snapshot.")
	dashSlug        = flag.String("dashboard_slug", "", "The url friendly version of the dashboard title to snapshot from the \"grafana_addr\" address.")
	snapshotExpires = flag.Duration("snapshot_expires", 0, "How long to keep the snapshot for (60s, 1h, 10d, etc), defaults to never.")
	snapshotName    = flag.String("snapshot_name", "", "What to call the snapshot. Defaults to \"from\" date plus dashboard slug.")
//	fromTimestamp   = flag.String("from", (time.Now().Truncate(time.Hour * 24)).Format(timeLayout), "The \"from\" time range. Must be absolute in the form \"YYYY-MM-DD HH:mm:ss\" (\"2017-01-23 12:34:56\"). Defaults to start of day.")
//	fromTimestamp   = flag.String("from", (time.Now().AddDate(0, 0, -1)).Format(timeLayout), "The \"from\" time range. Must be absolute in the form \"YYYY-MM-DD HH:mm:ss\" (\"2017-01-23 12:34:56\"). Defaults to start of day.")
	fromTimestamp   = flag.String("from", (time.Now().AddDate(0, 0, -1)).Format(timeLayout), "The \"from\" time range. Must be absolute in the form \"YYYY-MM-DD HH:mm:ss UTC\" (\"2017-01-23 12:34:56 UTC\"). Defaults to start of day.")

//	toTimestamp     = flag.String("to", time.Now().Format(timeLayout), "The \"to\" time range. Must be absolute in the form \"YYYY-MM-DD HH:mm:ss\" (\"2017-01-23 12:34:57\"). Must be greater than to \"to\" value. Defaults to now")
	toTimestamp     = flag.String("to", time.Now().Format(timeLayout), "The \"to\" time range. Must be absolute in the form \"YYYY-MM-DD HH:mm:ss UTC\" (\"2017-01-23 12:34:57 UTC\"). Must be greater than to \"to\" value. Defaults to now")

	templateVars    = flag.String("template_vars", "", "a list of key value pairs to set the dashboard's template variables, in the format 'key1=val1;key2=val2'")
)

func parseAndValidateFlags() (*snapshot.Config, *snapshot.TakeConfig, error) {
	flag.Parse()
	config := &snapshot.Config{}
	takeConfig := &snapshot.TakeConfig{}

	// Parse Grafana Address
	gURL, err := url.Parse(*grafanaAddr)
	if err != nil {
		return nil, nil, err
	}
	if !strings.HasSuffix(gURL.Path, "/") {
		gURL.Path = gURL.Path + "/"
	}
	config.GrafanaAddr = gURL

	// Grafana API key
	if len(*grafanaAPIKey) == 0 {
		return nil, nil, errors.New("\"grafana_api_key\" cannot be empty")
	}
	config.GrafanaAPIKey = *grafanaAPIKey

	// Parse Snapshot host Address
	if len(*snapshotAddr) == 0 {
		*snapshotAddr = *grafanaAddr
	}
	sURL, err := url.Parse(*snapshotAddr)
	if err != nil {
		return nil, nil, err
	}
	if !strings.HasSuffix(sURL.Path, "/") {
		sURL.Path = sURL.Path + "/"
	}
	config.SnapshotAddr = sURL

	// Snapshot API key
	if config.GrafanaAddr == config.SnapshotAddr {
		*snapshotAPIKey = *grafanaAPIKey
	}
	config.SnapshotAPIKey = *snapshotAPIKey

	// Dashboard slug
	if len(*dashSlug) == 0 {
		return nil, nil, errors.New("\"dashboard_slug\" cannot be empty")
	}
	if strings.Index(*dashSlug, " ") != -1 {
		return nil, nil, errors.New("\"dashboard_slug\" contained an invalid character: \" \"")
	}
	takeConfig.DashSlug = *dashSlug

	// Parse expiry
	takeConfig.Expires = *snapshotExpires

	// From timestamp

	from, err := time.Parse(timeLayout, *fromTimestamp)
	if err != nil {
		return nil, nil, err
	}
	takeConfig.From = &from
	// To timestamp
	to, err := time.Parse(timeLayout, *toTimestamp)
	if err != nil {
		return nil, nil, err
	}
	takeConfig.To = &to

	// Parse name
	if len(*snapshotName) == 0 {
		*snapshotName = fmt.Sprintf("%s %s", takeConfig.To.Format("2006-01-02"), takeConfig.DashSlug)
	}
	takeConfig.SnapshotName = *snapshotName

	// Template vars
	takeConfig.Vars = make(map[string]string)
	for _, pairS := range strings.Split(*templateVars, ";") {
		if len(pairS) > 2 {
			pairA := strings.Split(pairS, "=")
			if len(pairA) != 2 {
				return nil, nil, errors.New("\"template_vars\" contained an invalid pairing: \"" + pairS + "\"")
			}

			takeConfig.Vars[pairA[0]] = pairA[1]
		}
	}

	return config, takeConfig, nil
}

func stderr(msg string) {
	os.Stderr.WriteString(msg + "\n")
}
func stdout(msg string) {
	os.Stdout.WriteString(msg + "\n")
}

func main() {

	// Configure
	config, takeConfig, err := parseAndValidateFlags()
	if err != nil {
		stderr(fmt.Sprintf("Failed to parse flags: %s", err.Error()))
		os.Exit(1)
	}

	snapclient, err := snapshot.NewSnapClient(config)
	if err != nil {
		stderr(fmt.Sprintf("Failed to create SnapClient: %s", err.Error()))
		os.Exit(1)
	}

	snapshot, err := snapclient.Take(takeConfig)
	if err != nil {
		stderr(fmt.Sprintf("Failed to take snapshot: %s", err.Error()))
		os.Exit(1)
	}

//	stdout(fmt.Sprintf("%s%s%s", config.GrafanaAddr.String(), "dashboard/snapshot/", snapshot.Key))
	stdout(fmt.Sprintf("%s%s%s%s", config.GrafanaAddr.String(), "dashboard/snapshot/", snapshot.Key,"?kiosk&theme=light"))
}
