// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

// Package check contains the client for the API exposed by system-probe
package check

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	sysprobeclient "github.com/DataDog/datadog-agent/pkg/system-probe/api/client"
	"github.com/DataDog/datadog-agent/pkg/system-probe/config/types"
	"github.com/DataDog/datadog-agent/pkg/telemetry"
	"github.com/DataDog/datadog-agent/pkg/util/funcs"
)

const (
	checkLabelName     = "check"
	telemetrySubsystem = "system_probe__remote_client"
)

// Client is a client for communicating with the system-probe check API
type Client struct {
	Client *http.Client
}

var checkTelemetry = struct {
	totalRequests      telemetry.Counter
	failedRequests     telemetry.Counter
	failedResponses    telemetry.Counter
	responseErrors     telemetry.Counter
	malformedResponses telemetry.Counter
}{
	telemetry.NewCounter(telemetrySubsystem, "requests__total", []string{checkLabelName}, "Counter measuring how many system-probe check requests were made"),
	telemetry.NewCounter(telemetrySubsystem, "requests__failed", []string{checkLabelName}, "Counter measuring how many system-probe check requests failed to be sent"),
	telemetry.NewCounter(telemetrySubsystem, "responses__not_received", []string{checkLabelName}, "Counter measuring how many responses from system-probe check were not read from the socket"),
	telemetry.NewCounter(telemetrySubsystem, "responses__errors", []string{checkLabelName}, "Counter measuring how many non_ok status code received from system-probe checks"),
	telemetry.NewCounter(telemetrySubsystem, "responses__malformed", []string{checkLabelName}, "Counter measuring how many malformed responses were received from system-probe checks"),
}

// Get returns a client used to communicate with the system-probe check API
var Get = funcs.MemoizeArgNoError(get)

func get(socketPath string) *Client {
	return &Client{
		Client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:          2,
				IdleConnTimeout:       sysprobeclient.IdleConnTimeout,
				DialContext:           sysprobeclient.DialContextFunc(socketPath),
				TLSHandshakeTimeout:   1 * time.Second,
				ResponseHeaderTimeout: 5 * time.Second,
				ExpectContinueTimeout: 50 * time.Millisecond,
			},
		},
	}
}

// GetCheck returns data unmarshalled from JSON to T, from the specified module at the /<module>/check endpoint.
func GetCheck[T any](client *Client, module types.ModuleName) (T, error) {
	checkTelemetry.totalRequests.IncWithTags(map[string]string{checkLabelName: string(module)})
	var data T
	req, err := http.NewRequest("GET", sysprobeclient.ModuleURL(module, "/check"), nil)
	if err != nil {
		//we don't have a counter for this case, because this function can't really fail, since ModuleURL function constructs a safe URL
		return data, err
	}

	resp, err := client.Client.Do(req)
	if err != nil {
		checkTelemetry.failedRequests.IncWithTags(map[string]string{checkLabelName: string(module)})
		return data, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		checkTelemetry.failedResponses.IncWithTags(map[string]string{checkLabelName: string(module)})
		return data, err
	}
	if resp.StatusCode != http.StatusOK {
		checkTelemetry.responseErrors.IncWithTags(map[string]string{checkLabelName: string(module)})
		return data, fmt.Errorf("non-ok status code: url %s, status_code: %d, response: `%s`", req.URL, resp.StatusCode, string(body))
	}

	err = json.Unmarshal(body, &data)
	if err != nil {
		checkTelemetry.malformedResponses.IncWithTags(map[string]string{checkLabelName: string(module)})
	}
	return data, err
}
