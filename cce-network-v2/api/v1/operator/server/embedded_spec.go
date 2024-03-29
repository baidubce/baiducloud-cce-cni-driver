// Code generated by go-swagger; DO NOT EDIT.

/*
 * Copyright (c) 2023 Baidu, Inc. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
 * except in compliance with the License. You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under the
 * License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions
 * and limitations under the License.
 *
 */

package server

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"encoding/json"
)

var (
	// SwaggerJSON embedded version of the swagger document used at generation time
	SwaggerJSON json.RawMessage
	// FlatSwaggerJSON embedded flattened version of the swagger document used at generation time
	FlatSwaggerJSON json.RawMessage
)

func init() {
	SwaggerJSON = json.RawMessage([]byte(`{
  "consumes": [
    "application/json"
  ],
  "produces": [
    "application/json"
  ],
  "swagger": "2.0",
  "info": {
    "description": "CCE",
    "title": "CCE Operator",
    "version": "v1beta"
  },
  "basePath": "/v1",
  "paths": {
    "/healthz": {
      "get": {
        "description": "This path will return the status of cce operator instance.",
        "produces": [
          "text/plain"
        ],
        "tags": [
          "operator"
        ],
        "summary": "Get health of CCE operator",
        "responses": {
          "200": {
            "description": "CCE operator is healthy",
            "schema": {
              "type": "string"
            }
          },
          "500": {
            "description": "CCE operator is not healthy",
            "schema": {
              "type": "string"
            }
          }
        }
      }
    },
    "/metrics/": {
      "get": {
        "tags": [
          "metrics"
        ],
        "summary": "Retrieve cce operator metrics",
        "responses": {
          "200": {
            "description": "Success",
            "schema": {
              "type": "array",
              "items": {
                "$ref": "../openapi.yaml#/definitions/Metric"
              }
            }
          },
          "500": {
            "description": "Metrics cannot be retrieved",
            "x-go-name": "Failed"
          }
        }
      }
    }
  },
  "x-schemes": [
    "unix"
  ]
}`))
	FlatSwaggerJSON = json.RawMessage([]byte(`{
  "consumes": [
    "application/json"
  ],
  "produces": [
    "application/json"
  ],
  "swagger": "2.0",
  "info": {
    "description": "CCE",
    "title": "CCE Operator",
    "version": "v1beta"
  },
  "basePath": "/v1",
  "paths": {
    "/healthz": {
      "get": {
        "description": "This path will return the status of cce operator instance.",
        "produces": [
          "text/plain"
        ],
        "tags": [
          "operator"
        ],
        "summary": "Get health of CCE operator",
        "responses": {
          "200": {
            "description": "CCE operator is healthy",
            "schema": {
              "type": "string"
            }
          },
          "500": {
            "description": "CCE operator is not healthy",
            "schema": {
              "type": "string"
            }
          }
        }
      }
    },
    "/metrics/": {
      "get": {
        "tags": [
          "metrics"
        ],
        "summary": "Retrieve cce operator metrics",
        "responses": {
          "200": {
            "description": "Success",
            "schema": {
              "type": "array",
              "items": {
                "$ref": "#/definitions/metric"
              }
            }
          },
          "500": {
            "description": "Metrics cannot be retrieved",
            "x-go-name": "Failed"
          }
        }
      }
    }
  },
  "definitions": {
    "metric": {
      "description": "Metric information",
      "type": "object",
      "properties": {
        "labels": {
          "description": "Labels of the metric",
          "type": "object",
          "additionalProperties": {
            "type": "string"
          }
        },
        "name": {
          "description": "Name of the metric",
          "type": "string"
        },
        "value": {
          "description": "Value of the metric",
          "type": "number"
        }
      }
    }
  },
  "x-schemes": [
    "unix"
  ]
}`))
}
