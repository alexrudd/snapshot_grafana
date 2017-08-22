package snapshot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"log"
	"github.com/prometheus/client_golang/api"
	"github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// SnapClient is for taking multiple snapshots of a Grafana instance and posting
// them to a snapshot host
type SnapClient struct {
	config          *Config
	datasourceCache map[string]interface{}
}

// Snapshot is returned on a successful Take call
type Snapshot struct {
	URL       string `json:"url"`
	Key       string `json:"key"`
	DeleteURL string `json:"deleteUrl"`
	DeleteKey string `json:"deleteKey"`
}

type snapshotData struct {
	Target     string          `json:"target"`
	Datapoints [][]interface{} `json:"datapoints"`
	// Metric is a set of labels (e.g. instance=alp) which is retained
	// so that we can replace labels according to target.legendFormat.
	Metric model.Metric `json:"-"`
}

// NewSnapClient takes a Config, validates it, and returns a SnapClient
func NewSnapClient(config *Config) (*SnapClient, error) {
	c, err := processConfig(config)
	if err != nil {
		return nil, err
	}
	return &SnapClient{c, nil}, nil
}

// Take is for taking a snapshot
// TODO: Should take context
func (sc *SnapClient) Take(config *TakeConfig) (*Snapshot, error) {
	// process and validate config
	c, err := processTakeConfig(config)
	if err != nil {
		return nil, err
	}

	// get dashboard
	rawDashString, err := sc.getDashboardDef(c)
	if err != nil {
		return nil, err
	}

	// Get available datasources and map them to their names
	datasourceMap, err := sc.getDatasourceDefs()
	if err != nil {
		return nil, err
	}
	sc.datasourceCache = datasourceMap

	// Replace all templated variables
	subbedDashString, err := sc.substituteVars(c, rawDashString)
	if err != nil {
		return nil, err
	}

	// Unmarshal it
	var dash map[string]interface{}
	if err = json.Unmarshal([]byte(subbedDashString), &dash); err != nil {
		return nil, fmt.Errorf("Could not decode dashboard json: %s", err.Error())
	}
	if dash["dashboard"] == nil {
		return nil, fmt.Errorf(dash["message"].(string))
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
				if target["interval"] != nil && target["interval"].(string) != "" {
					log.Printf(target["interval"].(string))
					interval, err = time.ParseDuration(target["interval"].(string))
				}
				if err != nil {
					return nil, err
				}
				step := interval.Seconds() * intervalFactor
				// Lookup datasource
				datasource := datasourceMap[datasourceName].(map[string]interface{})

				// Fetch data points from datasource proxy
				var dataPoints []snapshotData
				switch datasource["type"].(string) {
				case "prometheus":
					dataPoints, err = sc.fetchDataPointsPrometheus(c, target, datasource, step)
					if err != nil {
						return nil, err
					}
				case "elasticsearch":
					dataPoints, err = sc.fetchDataPointsElastic(c, target, datasource, step)
					if err != nil {
						return nil, err
					}
				default:
					// unsupported
					continue
				}
				var snapshotData []interface{}
				// build snapshot data
				for idx, dp := range dataPoints {
					if target["legendFormat"] != nil && target["legendFormat"].(string) != "" {
						dp.Target = sc.renderTemplate(target["legendFormat"].(string), dp.Metric)
					} else {
						dp.Target = dp.Metric.String()
					}
					dataPoints[idx] = dp
					snapshotData = append(snapshotData, dp)
				}
				if snapshotData == nil {
					snapshotData = []interface{}{}
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
	// remove templating
	dash["dashboard"].(map[string]interface{})["templating"].(map[string]interface{})["list"] = []interface{}{}
	// update time range
	dash["dashboard"].(map[string]interface{})["time"].(map[string]interface{})["from"] = c.From.Format(time.RFC3339Nano)
	dash["dashboard"].(map[string]interface{})["time"].(map[string]interface{})["to"] = c.To.Format(time.RFC3339Nano)
	snapshot["dashboard"] = dash["dashboard"]
	snapshot["expires"] = (c.Expires / time.Second)
	snapshot["name"] = c.SnapshotName
	b, err := json.Marshal(snapshot)

	// Post Snapshot
	reqURL := *sc.config.SnapshotAddr
	reqURL.Path = reqURL.Path + "api/snapshots"
	log.Printf("Posting snapshot to: %s", reqURL.String())

	req, err := http.NewRequest("post", reqURL.String(), bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Add("Authorization", "Bearer "+sc.config.SnapshotAPIKey)
	req.Header.Add("Content-Type", "application/json")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Unexpected status code when posting snapshot: %s", resp.Status)
	}
	// read body
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	// parse body
	var snapshotResponse Snapshot
	if err = json.Unmarshal(body, &snapshotResponse); err != nil {
		return nil, err
	}

	return &snapshotResponse, nil
}

func (sc *SnapClient) getDashboardDef(config *TakeConfig) (string, error) {
	// Get dashboard def
	reqURL := *sc.config.GrafanaAddr
	reqURL.Path = reqURL.Path + "api/dashboards/db/" + config.DashSlug

	req, err := http.NewRequest("get", reqURL.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Add("Authorization", "Bearer "+sc.config.GrafanaAPIKey)
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

func (sc *SnapClient) getDatasourceDefs() (map[string]interface{}, error) {
	// Get datasource defs
	reqURL := *sc.config.GrafanaAddr
	reqURL.Path = reqURL.Path + "api/datasources"

	req, err := http.NewRequest("get", reqURL.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Authorization", "Bearer "+sc.config.GrafanaAPIKey)
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

func (sc *SnapClient) substituteVars(config *TakeConfig, dashboardString string) (string, error) {
	for k, v := range config.Vars {
		vk := "$" + k
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

func (sc *SnapClient) fetchDataPointsPrometheus(config *TakeConfig, target, datasource map[string]interface{}, step float64) ([]snapshotData, error) {
	reqURL := *sc.config.GrafanaAddr
	reqURL.Path = reqURL.Path + "api/datasources/proxy/" + strconv.Itoa(int(datasource["id"].(float64)))
	log.Printf("Requesting data points from: %s", reqURL.String())

	// Use our Grafana proxy transport with configured API key
	transport := grafanaProxyTransport{grafanaAPIKey: sc.config.GrafanaAPIKey}
	client, err := api.NewClient(api.Config{Address: reqURL.String(), RoundTripper: &transport})
	if err != nil {
		return nil, err
	}
	api := v1.NewAPI(client)

	// Query
	val, err := api.QueryRange(context.Background(), target["expr"].(string), v1.Range{
		Start: *config.From,
		End:   *config.To,
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

func (sc *SnapClient) fetchDataPointsElastic(config *TakeConfig, target, datasource map[string]interface{}, step float64) ([]snapshotData, error) {
	return nil, nil
}

var aliasRe = regexp.MustCompile(`{{\s*(.+?)\s*}}`)

// renderTemplate is a re-implementation of renderTemplate in
// Grafana’s Prometheus datasource; for the original, see:
// https://github.com/grafana/grafana/blob/79138e211fac98bf1d12f1645ecd9fab5846f4fb/public/app/plugins/datasource/prometheus/datasource.ts#L263
func (sc *SnapClient) renderTemplate(format string, metric model.Metric) string {
	return aliasRe.ReplaceAllStringFunc(format, func(match string) string {
		matches := aliasRe.FindStringSubmatch(match)
		return string(metric[model.LabelName(matches[1])])
	})
}
