// Copyright (C) 2021-2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

package utils

import "encoding/json"

// Set k=v in JSON string
// e.g., "track-chains" is the key and value is "a,b,c".
func SetJSONKey(jsonBody string, k string, v string) (string, error) {
	var config map[string]interface{}

	if err := json.Unmarshal([]byte(jsonBody), &config); err != nil {
		return "", err
	}

	if v == "" {
		delete(config, k)
	} else {
		config[k] = v
	}

	updatedJSON, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	return string(updatedJSON), nil
}
