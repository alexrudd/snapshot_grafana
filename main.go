package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/api/prometheus"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
)

var (
	timeLayout      = "2006-01-02 15:04:05"
	grafanaAddr     = flag.String("grafana_addr", "http://localhost:3000/", "The address of the Grafana instance to snapshot.")
	grafanaAPIKey   = flag.String("grafana_api_key", "", "The address of the Grafana instance to snapshot.")
	dashSlug        = flag.String("dashboard_slug", "home", "The url friendly version of the dashboard title to snapshot from the \"grafana_addr\" address.")
	snapshotAddr    = flag.String("snapshot_addr", "", "The location to submit the snapshot. Defaults to the grafana address.")
	snapshotExpires = flag.Duration("snapshot_expires", 0, "How long to keep the snapshot for (60s, 1h, 10d, etc), defaults to never.")
	snapshotName    = flag.String("snapshot_name", "", "What to call the snapshot. Defaults to \"from\" date plus dashboard slug.")
	fromTimestamp   = flag.String("from", (time.Now().Truncate(time.Hour * 24)).Format(timeLayout), "The \"from\" time range. Must be absolute in the form \"YYYY-MM-DD HH:mm:ss\" (\"2017-01-23 12:34:56\"). Defaults to start of day.")
	toTimestamp     = flag.String("to", time.Now().Format(timeLayout), "The \"to\" time range. Must be absolute in the form \"YYYY-MM-DD HH:mm:ss\" (\"2017-01-23 12:34:57\"). Must be greater than to \"to\" value. Defaults to now")
	templateVars    = flag.String("template_vars", "", "a list of key value pairs to set the dashboard's template variables, in the format 'key1=val1;key2=val2'")
	debug           = flag.Bool("debug", false, "Turns on debug logging")
)

type config struct {
	grafanaAddr  url.URL
	apiKey       string
	dashSlug     string
	snapshotAddr url.URL
	snapshotName string
	from         time.Time
	to           time.Time
	vars         map[string]string
	expires      int
}

type snapshotData struct {
	Target     string          `json:"target"`
	Datapoints [][]interface{} `json:"datapoints"`
	// Metric is a set of labels (e.g. instance=alp) which is retained
	// so that we can replace labels according to target.legendFormat.
	Metric model.Metric `json:"-"`
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

	// Grafana API key
	if len(*grafanaAPIKey) == 0 {
		return nil, errors.New("\"grafana_api_key\" cannot be empty")
	}
	config.apiKey = *grafanaAPIKey

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
	if !strings.HasSuffix(sURL.Path, "/") {
		sURL.Path = sURL.Path + "/"
	}
	config.snapshotAddr = *sURL

	// Parse expiry
	config.expires = int(*snapshotExpires / time.Second)

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

	// Parse name
	if len(*snapshotName) == 0 {
		*snapshotName = fmt.Sprintf("%s %s", config.to.Format("2006-01-02"), config.dashSlug)
	}
	config.snapshotName = *snapshotName

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

func getDashboardDef(config *config) (string, error) {
	// Get dashboard def
	reqURL := config.grafanaAddr
	reqURL.Path = reqURL.Path + "api/dashboards/db/" + config.dashSlug
	log.Debugf("Requesting dashboard definition from: %s", reqURL.String())

	req, err := http.NewRequest("get", reqURL.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Add("Authorization", "Bearer "+config.apiKey)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func getDatasourceDefs(config *config) (map[string]interface{}, error) {
	// Get datasource defs
	reqURL := config.grafanaAddr
	reqURL.Path = reqURL.Path + "api/datasources"
	log.Debugf("Requesting datasource definitions from: %s", reqURL.String())

	req, err := http.NewRequest("get", reqURL.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Authorization", "Bearer "+config.apiKey)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errors.New("Unexpected status code: " + resp.Status)
	}
	// read body
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	// parse body
	var datasources []interface{}
	if err = json.Unmarshal(body, &datasources); err != nil {
		return nil, err
	}
	// map datasources to their names
	datasourceMap := make(map[string]interface{})
	for _, ds := range datasources {
		datasourceMap[ds.(map[string]interface{})["name"].(string)] = ds
	}

	return datasourceMap, nil
}

func substituteVars(config *config, dashboardString string) (string, error) {
	for k, v := range config.vars {
		vk := "$" + k
		log.Debugf("Replacing \"%s\" with \"%s\"", vk, v)
		dashboardString = strings.Replace(dashboardString, vk, v, -1)
	}
	return dashboardString, nil
}

// Implementation of CancelableTransport (https://gowalker.org/github.com/prometheus/client_golang/api/prometheus#CancelableTransport)
// Required to intercept the api requests and add the auth header for going
// through the Grafana datasource proxy
type grafanaProxyTransport struct {
	http.Transport
	grafanaAPIKey string
}

// Adds the Grafana API key auth header to any request
func (gpt *grafanaProxyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Add("Authorization", "Bearer "+gpt.grafanaAPIKey)
	return (&http.Transport{}).RoundTrip(req)
}

func fetchDataPointsPrometheus(config *config, target, datasource map[string]interface{}, step float64) ([]snapshotData, error) {
	reqURL := config.grafanaAddr
	reqURL.Path = reqURL.Path + "api/datasources/proxy/" + strconv.Itoa(int(datasource["id"].(float64)))
	log.Debugf("Requesting data points from: %s", reqURL.String())

	// Use our Grafana proxy transport with configured API key
	transport := grafanaProxyTransport{grafanaAPIKey: config.apiKey}
	client, err := prometheus.New(prometheus.Config{Address: reqURL.String(), Transport: &transport})
	if err != nil {
		return nil, err
	}
	api := prometheus.NewQueryAPI(client)

	// Query
	val, err := api.QueryRange(context.Background(), target["expr"].(string), prometheus.Range{
		Start: config.from,
		End:   config.to,
		Step:  time.Duration(step) * time.Second,
	})
	if err != nil {
		return nil, err
	}

	if val.Type() != model.ValMatrix {
		return nil, fmt.Errorf("Unexpected value type: got %q, want %q", val.Type(), model.ValMatrix)
	}
	matrix, ok := val.(model.Matrix)
	if !ok {
		return nil, fmt.Errorf("Bug: val.Type() == model.ValMatrix, but type assertion failed")
	}

	results := make([]snapshotData, matrix.Len())
	for idx, stream := range matrix {
		datapoints := make([][]interface{}, len(stream.Values))
		for idx, samplepair := range stream.Values {
			if math.IsNaN(float64(samplepair.Value)) {
				datapoints[idx] = []interface{}{nil, float64(samplepair.Timestamp)}
			} else {
				datapoints[idx] = []interface{}{float64(samplepair.Value), float64(samplepair.Timestamp)}
			}
		}

		results[idx] = snapshotData{
			Metric:     stream.Metric,
			Datapoints: datapoints,
		}
	}

	return results, nil
}

func fetchDataPointsElastic(config *config, target, datasource map[string]interface{}, step float64) ([]snapshotData, error) {
	return nil, nil
}

var aliasRe = regexp.MustCompile(`{{\s*(.+?)\s*}}`)

// renderTemplate is a re-implementation of renderTemplate in
// Grafana’s Prometheus datasource; for the original, see:
// https://github.com/grafana/grafana/blob/79138e211fac98bf1d12f1645ecd9fab5846f4fb/public/app/plugins/datasource/prometheus/datasource.ts#L263
func renderTemplate(format string, metric model.Metric) string {
	return aliasRe.ReplaceAllStringFunc(format, func(match string) string {
		matches := aliasRe.FindStringSubmatch(match)
		return string(metric[model.LabelName(matches[1])])
	})
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
	log.Infof("Grafana API Key: %s", config.apiKey)
	log.Infof("Dashboard Slug: %s", config.dashSlug)
	log.Infof("Snapshot Address: %s", config.snapshotAddr.String())
	log.Infof("From Timestamp: %s", config.from.String())
	log.Infof("To Timestamp: %s", config.to.String())
	log.Infof("Template Vars:")
	for k, v := range config.vars {
		log.Infof("  %s = %s", k, v)
	}

	// Get the dashboard to snapshot
	rawDashString, err := getDashboardDef(config)
	if err != nil {
		log.Error(err)
		log.Exit(1)
	}
	log.Infof("Fetched dashboard definition for \"%s\"", config.dashSlug)

	// Get available datasources and map them to their names
	datasourceMap, err := getDatasourceDefs(config)
	if err != nil {
		log.Error(err)
		log.Exit(1)
	}
	log.Info("Fetched datasource definitions")

	// Replace all templated variables
	subbedDashString, err := substituteVars(config, rawDashString)
	if err != nil {
		log.Error(err)
		log.Exit(1)
	}
	log.Infof("Substituted dashboard variables")

	// Unmarshal it to a simplejson object
	var dash map[string]interface{}
	if err = json.Unmarshal([]byte(subbedDashString), &dash); err != nil {
		log.Errorf("Could not decode dashboard json: %s", err.Error())
		log.Exit(1)
	}

	// For each row in dashboard...
	for _, row := range dash["dashboard"].(map[string]interface{})["rows"].([]interface{}) {
		// For each panel in row...
		for _, p := range row.(map[string]interface{})["panels"].([]interface{}) {
			panel := p.(map[string]interface{})
			// Get the datasource and targets
			datasourceName := panel["datasource"].(string)
			targets := panel["targets"].([]interface{})
			// For each target in panel...
			for _, t := range targets {
				target := t.(map[string]interface{})
				// Calculate “step” like Grafana. For the original code, see:
				// https://github.com/grafana/grafana/blob/79138e211fac98bf1d12f1645ecd9fab5846f4fb/public/app/plugins/datasource/prometheus/datasource.ts#L83
				intervalFactor := float64(1)
				if target["intervalFactor"] != nil {
					intervalFactor = target["intervalFactor"].(float64)
				}
				interval := time.Second * 30
				if target["interval"] != nil {
					interval, err = time.ParseDuration(target["interval"].(string))
				}
				if err != nil {
					log.Error(err)
					log.Exit(1)
				}
				step := interval.Seconds() * intervalFactor
				// Lookup datasource
				datasource := datasourceMap[datasourceName].(map[string]interface{})

				// Fetch data points from datasource proxy
				var dataPoints []snapshotData
				switch datasource["type"].(string) {
				case "prometheus":
					dataPoints, err = fetchDataPointsPrometheus(config, target, datasource, step)
					if err != nil {
						log.Error(err)
						log.Exit(1)
					}
				case "elasticsearch":
					dataPoints, err = fetchDataPointsElastic(config, target, datasource, step)
					if err != nil {
						log.Error(err)
						log.Exit(1)
					}
				default:
					log.Errorf("Unsupported datasource type: %s", datasource["type"].(string))
					continue
				}
				var snapshotData []interface{}
				// build snapshot data
				for idx, dp := range dataPoints {
					if target["legendFormat"] != nil && target["legendFormat"].(string) != "" {
						dp.Target = renderTemplate(target["legendFormat"].(string), dp.Metric)
					} else {
						dp.Target = dp.Metric.String()
					}
					dataPoints[idx] = dp
					snapshotData = append(snapshotData, dp)
				}
				// insert snapshot data into panels
				panel["snapshotData"] = snapshotData
				panel["targets"] = []interface{}{}
				panel["links"] = []interface{}{}
				panel["datasource"] = []interface{}{}
			}
		}
	}

	// Build Snapshot
	snapshot := make(map[string]interface{})
	snapshot["dashboard"] = dash["dashboard"]
	snapshot["expires"] = config.expires
	snapshot["name"] = config.snapshotName
	b, err := json.Marshal(snapshot)

	// Post Snapshot
	reqURL := config.snapshotAddr
	reqURL.Path = reqURL.Path + "api/snapshots"
	log.Debugf("Posting snapshot to: %s", reqURL.String())

	req, err := http.NewRequest("post", reqURL.String(), bytes.NewReader(b))
	if err != nil {
		log.Error(err)
		log.Exit(1)
	}
	req.Header.Add("Authorization", "Bearer "+config.apiKey)
	req.Header.Add("Content-Type", "application/json")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		log.Error(err)
		log.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Errorf("Unexpected status code: %s", resp.Status)
		log.Exit(1)
	}
	// read body
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Error(err)
		log.Exit(1)
	}
	// parse body
	var snapshotResponse map[string]interface{}
	if err = json.Unmarshal(body, &snapshotResponse); err != nil {
		log.Error(err)
		log.Exit(1)
	}

	log.Infof("Snapshot URL: %s", snapshotResponse["url"].(string))
	log.Infof("Snapshot Key: %s", snapshotResponse["key"].(string))
	log.Infof("Snapshot Delete URL: %s", snapshotResponse["deleteUrl"].(string))
	log.Infof("Snapshot Delete Key: %s", snapshotResponse["deleteKey"].(string))
}
