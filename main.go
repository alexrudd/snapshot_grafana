package main

import (
	"errors"
	"flag"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

var (
	timeLayout    = "2006-01-02 15:04:05"
	grafanaAddr   = flag.String("grafana_addr", "http://localhost:3000/", "The address of the Grafana instance to snapshot.")
	dashSlug      = flag.String("dashboard_slug", "home", "The url friendly version of the dashboard title to snapshot from the \"grafana_addr\" address.")
	snapshotAddr  = flag.String("snapshot_addr", *grafanaAddr, "The location to submit the snapshot. Defaults to the grafana address.")
	fromTimestamp = flag.String("from", (time.Now().Truncate(time.Hour * 24)).Format(timeLayout), "The \"from\" time range. Must be absolute in the form \"YYYY-MM-DD HH:mm:ss\" (\"2017-01-23 12:34:56\"). Defaults to start of day.")
	toTimestamp   = flag.String("to", time.Now().Format(timeLayout), "The \"to\" time range. Must be absolute in the form \"YYYY-MM-DD HH:mm:ss\" (\"2017-01-23 12:34:57\"). Must be greater than to \"to\" value. Defaults to now")
	templateVars  = flag.String("template_vars", "", "a list of key value pairs to set the dashboard's template variables, in the format 'key1=val1;key2=val2'")
	debug         = flag.Bool("debug", false, "Turns on debug logging")
)

type config struct {
	grafanaAddr  url.URL
	dashSlug     string
	snapshotAddr url.URL
	from         time.Time
	to           time.Time
	vars         map[string]string
}

func parseAndValidateFlags() (*config, error) {
	flag.Parse()
	config := &config{}

	if *debug {
		log.SetLevel(log.DebugLevel)
	}

	// Parse Grafana Address
	gURL, err := url.Parse(*grafanaAddr)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(gURL.Path, "/") {
		gURL.Path = gURL.Path + "/"
	}
	config.grafanaAddr = *gURL

	// Dashboard slug
	if strings.Index(*dashSlug, " ") != -1 {
		return nil, errors.New("\"dashboard_slug\" contained an invalid character: \" \"")
	}
	config.dashSlug = *dashSlug

	// Parse Snapshot host Address
	if len(*snapshotAddr) == 0 {
		*snapshotAddr = *grafanaAddr
	}
	sURL, err := url.Parse(*snapshotAddr)
	if err != nil {
		return nil, err
	}
	config.snapshotAddr = *sURL

	// From timestamp
	from, err := time.Parse(timeLayout, *fromTimestamp)
	if err != nil {
		return nil, err
	}
	config.from = from
	// To timestamp
	to, err := time.Parse(timeLayout, *toTimestamp)
	if err != nil {
		return nil, err
	}
	config.to = to

	// Template vars
	config.vars = make(map[string]string)
	for _, pairS := range strings.Split(*templateVars, ";") {
		if len(pairS) > 2 {
			pairA := strings.Split(pairS, "=")
			if len(pairA) != 2 {
				return nil, errors.New("\"template_vars\" contained an invalid pairing: \"" + pairS + "\"")
			}

			config.vars[pairA[0]] = pairA[1]
		}
	}

	return config, nil
}

func main() {
	// Configure
	config, err := parseAndValidateFlags()
	if err != nil {
		log.Error(err)
		log.Exit(1)
	}
	log.Info("Snapshot config:")
	log.Infof("Grafana Address: %s", config.grafanaAddr.String())
	log.Infof("Dashboard Slug: %s", config.dashSlug)
	log.Infof("Snapshot Address: %s", config.snapshotAddr.String())
	log.Infof("From Timestamp: %s", config.from.String())
	log.Infof("To Timestamp: %s", config.to.String())
	log.Infof("Template Vars:")
	for k, v := range config.vars {
		log.Infof("  %s = %s", k, v)
	}

	// Get dashboard def
	reqURL := config.grafanaAddr
	reqURL.Path = reqURL.Path + "api/dashboards/db/" + config.dashSlug
	log.Debugf("Requesting dashboard definition from: %s", reqURL.String())
	resp, err := http.Get(reqURL.String())
	if err != nil {
		log.Error(err)
		log.Exit(1)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	log.Infof("%s", string(body))

}
