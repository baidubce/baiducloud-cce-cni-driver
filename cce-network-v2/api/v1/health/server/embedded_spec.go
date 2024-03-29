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
    "description": "CCE Health Checker",
    "title": "CCE-Health API",
    "version": "v1beta"
  },
  "basePath": "/v1beta",
  "paths": {
    "/healthz": {
      "get": {
        "description": "Returns health and status information of the local node including\nload and uptime, as well as the status of related components including\nthe CCE daemon.\n",
        "summary": "Get health of CCE node",
        "responses": {
          "200": {
            "description": "Success",
            "schema": {
              "$ref": "#/definitions/HealthResponse"
            }
          },
          "500": {
            "description": "Failed to contact local CCE daemon",
            "schema": {
              "$ref": "../openapi.yaml#/definitions/Error"
            },
            "x-go-name": "Failed"
          }
        }
      }
    },
    "/status": {
      "get": {
        "description": "Returns the connectivity status to all other cce-health instances\nusing interval-based probing.\n",
        "tags": [
          "connectivity"
        ],
        "summary": "Get connectivity status of the CCE cluster",
        "responses": {
          "200": {
            "description": "Success",
            "schema": {
              "$ref": "#/definitions/HealthStatusResponse"
            }
          }
        }
      }
    },
    "/status/probe": {
      "put": {
        "description": "Runs a synchronous probe to all other cce-health instances and\nreturns the connectivity status.\n",
        "tags": [
          "connectivity"
        ],
        "summary": "Run synchronous connectivity probe to determine status of the CCE cluster",
        "responses": {
          "200": {
            "description": "Success",
            "schema": {
              "$ref": "#/definitions/HealthStatusResponse"
            }
          },
          "500": {
            "description": "Internal error occurred while conducting connectivity probe",
            "schema": {
              "$ref": "../openapi.yaml#/definitions/Error"
            },
            "x-go-name": "Failed"
          }
        }
      }
    }
  },
  "definitions": {
    "ConnectivityStatus": {
      "description": "Connectivity status of a path",
      "type": "object",
      "properties": {
        "latency": {
          "description": "Round trip time to node in nanoseconds",
          "type": "integer"
        },
        "status": {
          "description": "Human readable status/error/warning message",
          "type": "string"
        }
      }
    },
    "EndpointStatus": {
      "description": "Connectivity status to host cce-health endpoints via different paths\n",
      "properties": {
        "primary-address": {
          "$ref": "#/definitions/PathStatus"
        },
        "secondary-addresses": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/PathStatus"
          }
        }
      }
    },
    "HealthResponse": {
      "description": "Health and status information of local node",
      "type": "object",
      "properties": {
        "cce": {
          "description": "Status of CCE daemon",
          "$ref": "#/definitions/StatusResponse"
        },
        "system-load": {
          "description": "System load on node",
          "$ref": "#/definitions/LoadResponse"
        },
        "uptime": {
          "description": "Uptime of cce-health instance",
          "type": "string"
        }
      }
    },
    "HealthStatusResponse": {
      "description": "Connectivity status to other daemons",
      "type": "object",
      "properties": {
        "local": {
          "description": "Description of the local node",
          "$ref": "#/definitions/SelfStatus"
        },
        "nodes": {
          "description": "Connectivity status to each other node",
          "type": "array",
          "items": {
            "$ref": "#/definitions/NodeStatus"
          }
        },
        "timestamp": {
          "type": "string"
        }
      }
    },
    "HostStatus": {
      "description": "Connectivity status to host cce-health instance via different paths,\nprobing via all known IP addresses\n",
      "properties": {
        "primary-address": {
          "$ref": "#/definitions/PathStatus"
        },
        "secondary-addresses": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/PathStatus"
          }
        }
      }
    },
    "LoadResponse": {
      "description": "System load on node",
      "type": "object",
      "properties": {
        "last15min": {
          "description": "Load average over the past 15 minutes",
          "type": "string"
        },
        "last1min": {
          "description": "Load average over the past minute",
          "type": "string"
        },
        "last5min": {
          "description": "Load average over the past 5 minutes",
          "type": "string"
        }
      }
    },
    "NodeStatus": {
      "description": "Connectivity status of a remote cce-health instance",
      "type": "object",
      "properties": {
        "endpoint": {
          "description": "DEPRECATED: Please use the health-endpoint field instead, which\nsupports reporting the status of different addresses for the endpoint\n",
          "$ref": "#/definitions/PathStatus"
        },
        "health-endpoint": {
          "description": "Connectivity status to simulated endpoint on the node",
          "$ref": "#/definitions/EndpointStatus"
        },
        "host": {
          "description": "Connectivity status to cce-health instance on node IP",
          "$ref": "#/definitions/HostStatus"
        },
        "name": {
          "description": "Identifying name for the node",
          "type": "string"
        }
      }
    },
    "PathStatus": {
      "description": "Connectivity status via different paths, for example using different\npolicies or service redirection\n",
      "type": "object",
      "properties": {
        "http": {
          "description": "Connectivity status without policy applied",
          "$ref": "#/definitions/ConnectivityStatus"
        },
        "icmp": {
          "description": "Basic ping connectivity status to node IP",
          "$ref": "#/definitions/ConnectivityStatus"
        },
        "ip": {
          "description": "IP address queried for the connectivity status",
          "type": "string"
        }
      }
    },
    "SelfStatus": {
      "description": "Description of the cce-health node",
      "type": "object",
      "properties": {
        "name": {
          "description": "Name associated with this node",
          "type": "string"
        }
      }
    },
    "StatusResponse": {
      "description": "Status of CCE daemon",
      "type": "object",
      "x-go-type": {
        "hint": {
          "kind": "object",
          "nullable": true
        },
        "import": {
          "alias": "cceModels",
          "package": "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
        },
        "type": "StatusResponse"
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
    "description": "CCE Health Checker",
    "title": "CCE-Health API",
    "version": "v1beta"
  },
  "basePath": "/v1beta",
  "paths": {
    "/healthz": {
      "get": {
        "description": "Returns health and status information of the local node including\nload and uptime, as well as the status of related components including\nthe CCE daemon.\n",
        "summary": "Get health of CCE node",
        "responses": {
          "200": {
            "description": "Success",
            "schema": {
              "$ref": "#/definitions/HealthResponse"
            }
          },
          "500": {
            "description": "Failed to contact local CCE daemon",
            "schema": {
              "$ref": "#/definitions/error"
            },
            "x-go-name": "Failed"
          }
        }
      }
    },
    "/status": {
      "get": {
        "description": "Returns the connectivity status to all other cce-health instances\nusing interval-based probing.\n",
        "tags": [
          "connectivity"
        ],
        "summary": "Get connectivity status of the CCE cluster",
        "responses": {
          "200": {
            "description": "Success",
            "schema": {
              "$ref": "#/definitions/HealthStatusResponse"
            }
          }
        }
      }
    },
    "/status/probe": {
      "put": {
        "description": "Runs a synchronous probe to all other cce-health instances and\nreturns the connectivity status.\n",
        "tags": [
          "connectivity"
        ],
        "summary": "Run synchronous connectivity probe to determine status of the CCE cluster",
        "responses": {
          "200": {
            "description": "Success",
            "schema": {
              "$ref": "#/definitions/HealthStatusResponse"
            }
          },
          "500": {
            "description": "Internal error occurred while conducting connectivity probe",
            "schema": {
              "$ref": "#/definitions/error"
            },
            "x-go-name": "Failed"
          }
        }
      }
    }
  },
  "definitions": {
    "ConnectivityStatus": {
      "description": "Connectivity status of a path",
      "type": "object",
      "properties": {
        "latency": {
          "description": "Round trip time to node in nanoseconds",
          "type": "integer"
        },
        "status": {
          "description": "Human readable status/error/warning message",
          "type": "string"
        }
      }
    },
    "EndpointStatus": {
      "description": "Connectivity status to host cce-health endpoints via different paths\n",
      "properties": {
        "primary-address": {
          "$ref": "#/definitions/PathStatus"
        },
        "secondary-addresses": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/PathStatus"
          }
        }
      }
    },
    "HealthResponse": {
      "description": "Health and status information of local node",
      "type": "object",
      "properties": {
        "cce": {
          "description": "Status of CCE daemon",
          "$ref": "#/definitions/StatusResponse"
        },
        "system-load": {
          "description": "System load on node",
          "$ref": "#/definitions/LoadResponse"
        },
        "uptime": {
          "description": "Uptime of cce-health instance",
          "type": "string"
        }
      }
    },
    "HealthStatusResponse": {
      "description": "Connectivity status to other daemons",
      "type": "object",
      "properties": {
        "local": {
          "description": "Description of the local node",
          "$ref": "#/definitions/SelfStatus"
        },
        "nodes": {
          "description": "Connectivity status to each other node",
          "type": "array",
          "items": {
            "$ref": "#/definitions/NodeStatus"
          }
        },
        "timestamp": {
          "type": "string"
        }
      }
    },
    "HostStatus": {
      "description": "Connectivity status to host cce-health instance via different paths,\nprobing via all known IP addresses\n",
      "properties": {
        "primary-address": {
          "$ref": "#/definitions/PathStatus"
        },
        "secondary-addresses": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/PathStatus"
          }
        }
      }
    },
    "LoadResponse": {
      "description": "System load on node",
      "type": "object",
      "properties": {
        "last15min": {
          "description": "Load average over the past 15 minutes",
          "type": "string"
        },
        "last1min": {
          "description": "Load average over the past minute",
          "type": "string"
        },
        "last5min": {
          "description": "Load average over the past 5 minutes",
          "type": "string"
        }
      }
    },
    "NodeStatus": {
      "description": "Connectivity status of a remote cce-health instance",
      "type": "object",
      "properties": {
        "endpoint": {
          "description": "DEPRECATED: Please use the health-endpoint field instead, which\nsupports reporting the status of different addresses for the endpoint\n",
          "$ref": "#/definitions/PathStatus"
        },
        "health-endpoint": {
          "description": "Connectivity status to simulated endpoint on the node",
          "$ref": "#/definitions/EndpointStatus"
        },
        "host": {
          "description": "Connectivity status to cce-health instance on node IP",
          "$ref": "#/definitions/HostStatus"
        },
        "name": {
          "description": "Identifying name for the node",
          "type": "string"
        }
      }
    },
    "PathStatus": {
      "description": "Connectivity status via different paths, for example using different\npolicies or service redirection\n",
      "type": "object",
      "properties": {
        "http": {
          "description": "Connectivity status without policy applied",
          "$ref": "#/definitions/ConnectivityStatus"
        },
        "icmp": {
          "description": "Basic ping connectivity status to node IP",
          "$ref": "#/definitions/ConnectivityStatus"
        },
        "ip": {
          "description": "IP address queried for the connectivity status",
          "type": "string"
        }
      }
    },
    "SelfStatus": {
      "description": "Description of the cce-health node",
      "type": "object",
      "properties": {
        "name": {
          "description": "Name associated with this node",
          "type": "string"
        }
      }
    },
    "StatusResponse": {
      "description": "Status of CCE daemon",
      "type": "object",
      "x-go-type": {
        "hint": {
          "kind": "object",
          "nullable": true
        },
        "import": {
          "alias": "cceModels",
          "package": "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
        },
        "type": "StatusResponse"
      }
    },
    "error": {
      "type": "string"
    }
  },
  "x-schemes": [
    "unix"
  ]
}`))
}
