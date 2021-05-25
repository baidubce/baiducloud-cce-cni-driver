/*
 * Copyright (c) 2021 Baidu, Inc. All Rights Reserved.
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

package v1

import (
	"sort"
	"testing"
	"time"
)

func TestENI_AssignedIPv4Addresses(t *testing.T) {
	type fields struct {
		ID            string
		createTime    time.Time
		IPv4Addresses map[string]*AddressInfo
	}
	tests := []struct {
		name   string
		fields fields
		want   int
	}{
		{
			fields: fields{
				IPv4Addresses: map[string]*AddressInfo{
					"10.0.0.2": &AddressInfo{Assigned: false},
					"10.0.0.3": &AddressInfo{Assigned: true},
				},
			},
			want: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &ENI{
				ID:            tt.fields.ID,
				IPv4Addresses: tt.fields.IPv4Addresses,
			}
			if got := e.AssignedIPv4Addresses(); got != tt.want {
				t.Errorf("AssignedIPv4Addresses() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestENI_TotalIPv4Addresses(t *testing.T) {
	type fields struct {
		ID            string
		createTime    time.Time
		IPv4Addresses map[string]*AddressInfo
	}
	tests := []struct {
		name   string
		fields fields
		want   int
	}{
		{
			fields: fields{
				IPv4Addresses: map[string]*AddressInfo{
					"10.0.0.2": &AddressInfo{Assigned: false},
					"10.0.0.3": &AddressInfo{Assigned: true},
				},
			},
			want: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &ENI{
				ID:            tt.fields.ID,
				IPv4Addresses: tt.fields.IPv4Addresses,
			}
			if got := e.TotalIPv4Addresses(); got != tt.want {
				t.Errorf("TotalIPv4Addresses() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAddressInfo_inCoolingPeriod(t *testing.T) {
	type fields struct {
		addr AddressInfo
	}
	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{
			name: "AfterCooling",
			fields: fields{
				addr: AddressInfo{
					UnassignedTime: time.Now().Add(-(addressCoolingPeriod + time.Second)),
				},
			},
			want: false,
		},
		{
			name: "BeforeCooling",
			fields: fields{
				addr: AddressInfo{
					UnassignedTime: time.Now(),
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fields.addr.inCoolingPeriod(); got != tt.want {
				t.Errorf("AddressInfo inCoolingPeriod() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDataStore_AllocatePodPrivateIP(t *testing.T) {
	dataStoreNormal := DataStore{
		store: map[string]*Instance{
			"instance_A": {
				ID:       "instance_A",
				total:    3,
				assigned: 1,
				eniPool: map[string]*ENI{
					"ENI_A": {
						ID: "ENI_A",
						IPv4Addresses: map[string]*AddressInfo{
							"10.1.2.3": {
								Address:  "10.1.2.3",
								Assigned: true,
							},
							"10.1.2.4": {
								Address:        "10.1.2.4",
								Assigned:       false,
								UnassignedTime: time.Now(),
							},
							"10.1.2.5": {
								Address:        "10.1.2.5",
								Assigned:       false,
								UnassignedTime: time.Now().Add(-(addressCoolingPeriod + time.Second)),
							},
						},
					},
				},
			},
		},
	}
	dataStoreFull := DataStore{
		store: map[string]*Instance{
			"instance_A": {
				ID:       "instance_A",
				total:    2,
				assigned: 2,
				eniPool: map[string]*ENI{
					"ENI_A": {
						ID: "ENI_A",
						IPv4Addresses: map[string]*AddressInfo{
							"10.1.2.3": {
								Address:  "10.1.2.3",
								Assigned: true,
							},
							"10.1.2.4": {
								Address:  "10.1.2.4",
								Assigned: true,
							},
						},
					},
				},
			},
		},
	}
	dataStoreWithUncooling := DataStore{
		store: map[string]*Instance{
			"instance_A": {
				ID:       "instance_A",
				total:    2,
				assigned: 1,
				eniPool: map[string]*ENI{
					"ENI_A": {
						ID: "ENI_A",
						IPv4Addresses: map[string]*AddressInfo{
							"10.1.2.3": {
								Address:  "10.1.2.3",
								Assigned: true,
							},
							"10.1.2.4": {
								Address:        "10.1.2.4",
								Assigned:       false,
								UnassignedTime: time.Now(),
							},
						},
					},
				},
			},
		},
	}
	type fields struct {
		dataStore DataStore
		node      string
	}
	tests := []struct {
		name        string
		fields      fields
		wantAddress string
		wantErr     bool
	}{
		{
			name: "Normal",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_A",
			},
			wantAddress: "10.1.2.5",
			wantErr:     false,
		},
		{
			name: "Node Not Exist",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_NOT_EXIST",
			},
			wantAddress: "",
			wantErr:     true,
		},
		{
			name: "Node ENI-Pool Full",
			fields: fields{
				dataStore: dataStoreFull,
				node:      "instance_A",
			},
			wantAddress: "",
			wantErr:     true,
		},
		{
			name: "Node ENI-Pool Not Cooling Adress",
			fields: fields{
				dataStore: dataStoreWithUncooling,
				node:      "instance_A",
			},
			wantAddress: "",
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotAddreess, gotErr := tt.fields.dataStore.AllocatePodPrivateIP(tt.fields.node)
			if gotAddreess != tt.wantAddress || (gotErr != nil) != tt.wantErr {
				t.Errorf("DataStore AllocatePodPrivateIP() = %v,%v, want %v,%v", gotAddreess, gotErr, tt.wantAddress, tt.wantErr)
			}
		})
	}
}

func TestDataStore_ReleasePodPrivateIP(t *testing.T) {
	dataStoreNormal := DataStore{
		store: map[string]*Instance{
			"instance_A": {
				ID:       "instance_A",
				total:    3,
				assigned: 1,
				eniPool: map[string]*ENI{
					"ENI_A": {
						ID: "ENI_A",
						IPv4Addresses: map[string]*AddressInfo{
							"10.1.2.3": {
								Address:  "10.1.2.3",
								Assigned: true,
							},
							"10.1.2.4": {
								Address:        "10.1.2.4",
								Assigned:       false,
								UnassignedTime: time.Now(),
							},
							"10.1.2.5": {
								Address:        "10.1.2.5",
								Assigned:       false,
								UnassignedTime: time.Now().Add(-(addressCoolingPeriod + time.Second)),
							},
						},
					},
				},
			},
		},
	}
	type fields struct {
		dataStore DataStore
		node      string
		eniID     string
		ip        string
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			name: "Normal",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_A",
				eniID:     "ENI_A",
				ip:        "10.1.2.3",
			},
			wantErr: false,
		},
		{
			name: "Node Not Exist",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_NOT_EXIST",
				eniID:     "ENI_A",
				ip:        "10.1.2.3",
			},
			wantErr: true,
		},
		{
			name: "ENI Not Exist",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_A",
				eniID:     "ENI_NOT_EXIST",
				ip:        "10.1.2.3",
			},
			wantErr: true,
		},
		{
			name: "IP Not Found ",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_A",
				eniID:     "ENI_A",
				ip:        "10.1.2.255",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotErr := tt.fields.dataStore.ReleasePodPrivateIP(tt.fields.node, tt.fields.eniID, tt.fields.ip)
			if (gotErr != nil) != tt.wantErr {
				t.Errorf("DataStore ReleasePodPrivateIP() = %v, want %v", gotErr, tt.wantErr)
			}
		})
	}
}

func TestDataStore_AddPrivateIPToStore(t *testing.T) {
	dataStoreNormal := DataStore{
		store: map[string]*Instance{
			"instance_A": {
				ID:       "instance_A",
				total:    3,
				assigned: 1,
				eniPool: map[string]*ENI{
					"ENI_A": {
						ID: "ENI_A",
						IPv4Addresses: map[string]*AddressInfo{
							"10.1.2.3": {
								Address:  "10.1.2.3",
								Assigned: true,
							},
							"10.1.2.4": {
								Address:        "10.1.2.4",
								Assigned:       false,
								UnassignedTime: time.Now(),
							},
							"10.1.2.5": {
								Address:        "10.1.2.5",
								Assigned:       false,
								UnassignedTime: time.Now().Add(-(addressCoolingPeriod + time.Second)),
							},
						},
					},
				},
			},
		},
	}
	type fields struct {
		dataStore DataStore
		node      string
		eniID     string
		ipAddress string
		assigned  bool
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			name: "Normal with assigned=true",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_A",
				eniID:     "ENI_A",
				ipAddress: "10.1.2.100",
				assigned:  true,
			},
			wantErr: false,
		},
		{
			name: "Normal with assigned=false",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_A",
				eniID:     "ENI_A",
				ipAddress: "10.1.2.101",
				assigned:  false,
			},
			wantErr: false,
		},
		{
			name: "Node Not Found",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_NOT_FOUND",
				eniID:     "ENI_A",
				ipAddress: "10.1.2.102",
				assigned:  false,
			},
			wantErr: true,
		},
		{
			name: "ENI Not Found",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_A",
				eniID:     "ENI_NOT_FOUND",
				ipAddress: "10.1.2.100",
				assigned:  false,
			},
			wantErr: true,
		},
		{
			name: "IP Address already exists",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_A",
				eniID:     "ENI_A",
				ipAddress: "10.1.2.3",
				assigned:  false,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotErr := tt.fields.dataStore.AddPrivateIPToStore(tt.fields.node, tt.fields.eniID, tt.fields.ipAddress, tt.fields.assigned)
			if (gotErr != nil) != tt.wantErr {
				t.Errorf("DataStore AddPrivateIPToStore() = %v, want %v", gotErr, tt.wantErr)
			}
		})
	}
}

func TestDataStore_DeletePrivateIPFromStore(t *testing.T) {
	dataStoreNormal := DataStore{
		store: map[string]*Instance{
			"instance_A": {
				ID:       "instance_A",
				total:    3,
				assigned: 1,
				eniPool: map[string]*ENI{
					"ENI_A": {
						ID: "ENI_A",
						IPv4Addresses: map[string]*AddressInfo{
							"10.1.2.3": {
								Address:  "10.1.2.3",
								Assigned: true,
							},
							"10.1.2.4": {
								Address:        "10.1.2.4",
								Assigned:       false,
								UnassignedTime: time.Now(),
							},
							"10.1.2.5": {
								Address:        "10.1.2.5",
								Assigned:       false,
								UnassignedTime: time.Now().Add(-(addressCoolingPeriod + time.Second)),
							},
						},
					},
				},
			},
		},
	}
	type fields struct {
		dataStore DataStore
		node      string
		eniID     string
		ipAddress string
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			name: "Normal",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_A",
				eniID:     "ENI_A",
				ipAddress: "10.1.2.5",
			},
			wantErr: false,
		},
		{
			name: "Node Not Found",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_NOT_FOUND",
				eniID:     "ENI_A",
				ipAddress: "10.1.2.5",
			},
			wantErr: true,
		},
		{
			name: "ENI Not Found",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_A",
				eniID:     "ENI_NOT_FOUND",
				ipAddress: "10.1.2.5",
			},
			wantErr: true,
		},
		{
			name: "IP Address Not Exist",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_A",
				eniID:     "ENI_A",
				ipAddress: "10.1.2.255",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotErr := tt.fields.dataStore.DeletePrivateIPFromStore(tt.fields.node, tt.fields.eniID, tt.fields.ipAddress)
			if (gotErr != nil) != tt.wantErr {
				t.Errorf("DataStore NodeExistsInStore() = %v, want %v", gotErr, tt.wantErr)
			}
		})
	}
}

func TestDataStore_AddNodeToStore(t *testing.T) {
	dataStoreNormal := DataStore{
		store: map[string]*Instance{
			"instance_A": {
				ID:       "instance_A",
				total:    3,
				assigned: 1,
				eniPool: map[string]*ENI{
					"ENI_A": {
						ID: "ENI_A",
						IPv4Addresses: map[string]*AddressInfo{
							"10.1.2.3": {
								Address:  "10.1.2.3",
								Assigned: true,
							},
							"10.1.2.4": {
								Address:        "10.1.2.4",
								Assigned:       false,
								UnassignedTime: time.Now(),
							},
							"10.1.2.5": {
								Address:        "10.1.2.5",
								Assigned:       false,
								UnassignedTime: time.Now().Add(-(addressCoolingPeriod + time.Second)),
							},
						},
					},
				},
			},
		},
	}
	type fields struct {
		dataStore  DataStore
		node       string
		instanceID string
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			name: "Normal",
			fields: fields{
				dataStore:  dataStoreNormal,
				node:       "instance_B",
				instanceID: "instance_B",
			},
			wantErr: false,
		},
		{
			name: "Node already exists",
			fields: fields{
				dataStore:  dataStoreNormal,
				node:       "instance_A",
				instanceID: "instance_A",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotErr := tt.fields.dataStore.AddNodeToStore(tt.fields.node, tt.fields.instanceID)
			if (gotErr != nil) != tt.wantErr {
				t.Errorf("DataStore NodeExistsInStore() = %v, want %v", gotErr, tt.wantErr)
			}
		})
	}
}

func TestDataStore_NodeExistsInStore(t *testing.T) {
	dataStoreNormal := DataStore{
		store: map[string]*Instance{
			"instance_A": {
				ID:       "instance_A",
				total:    3,
				assigned: 1,
				eniPool: map[string]*ENI{
					"ENI_A": {
						ID: "ENI_A",
						IPv4Addresses: map[string]*AddressInfo{
							"10.1.2.3": {
								Address:  "10.1.2.3",
								Assigned: true,
							},
							"10.1.2.4": {
								Address:        "10.1.2.4",
								Assigned:       false,
								UnassignedTime: time.Now(),
							},
							"10.1.2.5": {
								Address:        "10.1.2.5",
								Assigned:       false,
								UnassignedTime: time.Now().Add(-(addressCoolingPeriod + time.Second)),
							},
						},
					},
				},
			},
		},
	}
	type fields struct {
		dataStore DataStore
		node      string
	}
	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{
			name: "Normal",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_A",
			},
			want: true,
		},
		{
			name: "Normal",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_NOT_FOUND",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fields.dataStore.NodeExistsInStore(tt.fields.node)
			if got != tt.want {
				t.Errorf("DataStore NodeExistsInStore() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDataStore_DeleteNodeFromStore(t *testing.T) {
	dataStoreNormal := DataStore{
		store: map[string]*Instance{
			"instance_A": {
				ID:       "instance_A",
				total:    3,
				assigned: 1,
				eniPool: map[string]*ENI{
					"ENI_A": {
						ID: "ENI_A",
						IPv4Addresses: map[string]*AddressInfo{
							"10.1.2.3": {
								Address:  "10.1.2.3",
								Assigned: true,
							},
							"10.1.2.4": {
								Address:        "10.1.2.4",
								Assigned:       false,
								UnassignedTime: time.Now(),
							},
							"10.1.2.5": {
								Address:        "10.1.2.5",
								Assigned:       false,
								UnassignedTime: time.Now().Add(-(addressCoolingPeriod + time.Second)),
							},
						},
					},
				},
			},
		},
	}
	type fields struct {
		dataStore DataStore
		node      string
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			name: "Normal",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_A",
			},
			wantErr: false,
		},
		{
			name: "Instance Not Exist",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_NOT_EXIST",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotErr := tt.fields.dataStore.DeleteNodeFromStore(tt.fields.node)
			if (gotErr != nil) != tt.wantErr {
				t.Errorf("DataStore DeleteNodeFromStore() = %v, want %v", gotErr, tt.wantErr)
			}
		})
	}
}

func TestDataStore_AddENIToStore(t *testing.T) {
	dataStoreNormal := DataStore{
		store: map[string]*Instance{
			"instance_A": {
				ID:       "instance_A",
				total:    3,
				assigned: 1,
				eniPool: map[string]*ENI{
					"ENI_A": {
						ID: "ENI_A",
						IPv4Addresses: map[string]*AddressInfo{
							"10.1.2.3": {
								Address:  "10.1.2.3",
								Assigned: true,
							},
							"10.1.2.4": {
								Address:        "10.1.2.4",
								Assigned:       false,
								UnassignedTime: time.Now(),
							},
							"10.1.2.5": {
								Address:        "10.1.2.5",
								Assigned:       false,
								UnassignedTime: time.Now().Add(-(addressCoolingPeriod + time.Second)),
							},
						},
					},
				},
			},
		},
	}
	type fields struct {
		dataStore DataStore
		node      string
		eniID     string
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			name: "Normal",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_A",
				eniID:     "ENI_B",
			},
			wantErr: false,
		},
		{
			name: "Instance Not Found",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_NOT_EXIST",
				eniID:     "ENI_B",
			},
			wantErr: true,
		},
		{
			name: "ENI Duplicate",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_A",
				eniID:     "ENI_A",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotErr := tt.fields.dataStore.AddENIToStore(tt.fields.node, tt.fields.eniID)
			if (gotErr != nil) != tt.wantErr {
				t.Errorf("DataStore AddENIToStore() = %v, want %v", gotErr, tt.wantErr)
			}
		})
	}
}

func TestDataStore_ENIExistsInStore(t *testing.T) {
	dataStoreNormal := DataStore{
		store: map[string]*Instance{
			"instance_A": {
				ID:       "instance_A",
				total:    3,
				assigned: 1,
				eniPool: map[string]*ENI{
					"ENI_A": {
						ID: "ENI_A",
						IPv4Addresses: map[string]*AddressInfo{
							"10.1.2.3": {
								Address:  "10.1.2.3",
								Assigned: true,
							},
							"10.1.2.4": {
								Address:        "10.1.2.4",
								Assigned:       false,
								UnassignedTime: time.Now(),
							},
							"10.1.2.5": {
								Address:        "10.1.2.5",
								Assigned:       false,
								UnassignedTime: time.Now().Add(-(addressCoolingPeriod + time.Second)),
							},
						},
					},
				},
			},
		},
	}
	type fields struct {
		dataStore DataStore
		node      string
		eniID     string
	}
	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{
			name: "Normal",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_A",
				eniID:     "ENI_A",
			},
			want: true,
		},
		{
			name: "Instance Not Found",
			fields: fields{
				node:  "instance_NOT_FOUND",
				eniID: "ENI_A",
			},
			want: false,
		},
		{
			name: "ENI Not Found",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_A",
				eniID:     "ENI_NOT_FOUND",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fields.dataStore.ENIExistsInStore(tt.fields.node, tt.fields.eniID)
			if got != tt.want {
				t.Errorf("DataStore ENIExistsInStore() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDataStore_DeleteENIFromStore(t *testing.T) {
	dataStoreNormal := DataStore{
		store: map[string]*Instance{
			"instance_A": {
				ID:       "instance_A",
				total:    3,
				assigned: 1,
				eniPool: map[string]*ENI{
					"ENI_A": {
						ID: "ENI_A",
						IPv4Addresses: map[string]*AddressInfo{
							"10.1.2.3": {
								Address:  "10.1.2.3",
								Assigned: true,
							},
							"10.1.2.4": {
								Address:        "10.1.2.4",
								Assigned:       false,
								UnassignedTime: time.Now(),
							},
							"10.1.2.5": {
								Address:        "10.1.2.5",
								Assigned:       false,
								UnassignedTime: time.Now().Add(-(addressCoolingPeriod + time.Second)),
							},
						},
					},
				},
			},
		},
	}
	type fields struct {
		dataStore DataStore
		node      string
		eniID     string
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			name: "Normal",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_A",
				eniID:     "ENI_A",
			},
			wantErr: false,
		},
		{
			name: "Instance Not Exist",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_NOT_EXIST",
				eniID:     "ENI_A",
			},
			wantErr: true,
		},
		{
			name: "ENI Not Exist",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_A",
				eniID:     "ENI_NOT_EXIST",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotErr := tt.fields.dataStore.DeleteENIFromStore(tt.fields.node, tt.fields.eniID)
			if (gotErr != nil) != tt.wantErr {
				t.Errorf("DataStore DeleteENIFromStore() = %v, want %v", gotErr, tt.wantErr)
			}
		})
	}
}

func TestDataStore_GetNodeStats(t *testing.T) {
	dataStoreNormal := DataStore{
		store: map[string]*Instance{
			"instance_A": {
				ID:       "instance_A",
				total:    3,
				assigned: 1,
				eniPool: map[string]*ENI{
					"ENI_A": {
						ID: "ENI_A",
						IPv4Addresses: map[string]*AddressInfo{
							"10.1.2.3": {
								Address:  "10.1.2.3",
								Assigned: true,
							},
							"10.1.2.4": {
								Address:        "10.1.2.4",
								Assigned:       false,
								UnassignedTime: time.Now(),
							},
							"10.1.2.5": {
								Address:        "10.1.2.5",
								Assigned:       false,
								UnassignedTime: time.Now().Add(-(addressCoolingPeriod + time.Second)),
							},
						},
					},
				},
			},
		},
	}
	type fields struct {
		dataStore DataStore
		node      string
	}
	tests := []struct {
		name         string
		fields       fields
		wantAll      int
		wantAssigned int
		wantErr      bool
	}{
		{
			name: "Normal",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_A",
			},
			wantAll:      3,
			wantAssigned: 1,
			wantErr:      false,
		},
		{
			name: "Node Not Exist",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_NOT_EXIST",
			},
			wantAll:      0,
			wantAssigned: 0,
			wantErr:      true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotAll, gotAssigned, gotErr := tt.fields.dataStore.GetNodeStats(tt.fields.node)
			if gotAll != tt.wantAll || gotAssigned != tt.wantAssigned || (gotErr != nil) != tt.wantErr {
				t.Errorf("DataStore GetNodeStats() = %v,%v, %v, want %v,%v, %v", gotAll, gotAssigned, gotErr, tt.wantAll, tt.wantAssigned, tt.wantErr)
			}
		})
	}
}

func TestDataStore_GetUnassignedPrivateIPByNode(t *testing.T) {
	dataStoreNormal := DataStore{
		store: map[string]*Instance{
			"instance_A": {
				ID:       "instance_A",
				total:    3,
				assigned: 1,
				eniPool: map[string]*ENI{
					"ENI_A": {
						ID: "ENI_A",
						IPv4Addresses: map[string]*AddressInfo{
							"10.1.2.3": {
								Address:  "10.1.2.3",
								Assigned: true,
							},
							"10.1.2.4": {
								Address:        "10.1.2.4",
								Assigned:       false,
								UnassignedTime: time.Now(),
							},
							"10.1.2.5": {
								Address:        "10.1.2.5",
								Assigned:       false,
								UnassignedTime: time.Now().Add(-(addressCoolingPeriod + time.Second)),
							},
						},
					},
				},
			},
		},
	}
	dataStoreAllAssigned := DataStore{
		store: map[string]*Instance{
			"instance_A": {
				ID:       "instance_A",
				total:    3,
				assigned: 3,
				eniPool: map[string]*ENI{
					"ENI_A": {
						ID: "ENI_A",
						IPv4Addresses: map[string]*AddressInfo{
							"10.1.2.3": {
								Address:  "10.1.2.3",
								Assigned: true,
							},
							"10.1.2.4": {
								Address:  "10.1.2.4",
								Assigned: true,
							},
							"10.1.2.5": {
								Address:  "10.1.2.5",
								Assigned: true,
							},
						},
					},
				},
			},
		},
	}
	type fields struct {
		dataStore DataStore
		node      string
	}
	tests := []struct {
		name    string
		fields  fields
		want    []string
		wantErr bool
	}{
		{
			name: "Normal",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_A",
			},
			want:    []string{"10.1.2.4", "10.1.2.5"},
			wantErr: false,
		},
		{
			name: "Empty",
			fields: fields{
				dataStore: dataStoreNormal,
				node:      "instance_NOT_EXIST",
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "Empty List",
			fields: fields{
				dataStore: dataStoreAllAssigned,
				node:      "instance_A",
			},
			want:    []string{},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := tt.fields.dataStore.GetUnassignedPrivateIPByNode(tt.fields.node)
			if !IsStringsEqualWithoutOrder(got, tt.want) || (gotErr != nil) != tt.wantErr {
				t.Errorf("DataStore GetUnassignedPrivateIPByNode() = %v,%v, want %v,%v", got, gotErr, tt.want, tt.wantErr)
			}
		})
	}
}

func TestDataStore_ListNodes(t *testing.T) {
	dataStoreNormal := DataStore{
		store: map[string]*Instance{
			"instance_A": {},
			"instance_B": {},
		},
	}
	dataStoreEmpty := DataStore{
		store: map[string]*Instance{},
	}

	type fields struct {
		dataStore DataStore
	}

	tests := []struct {
		name   string
		fields fields
		want   []string
	}{
		{
			name: "Normal",
			fields: fields{
				dataStore: dataStoreNormal,
			},
			want: []string{"instance_A", "instance_B"},
		},
		{
			name: "Empty Node List",
			fields: fields{
				dataStore: dataStoreEmpty,
			},
			// TODO NodeList 为空时会传出一个 Nil 值
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fields.dataStore.ListNodes()
			if !IsStringsEqualWithoutOrder(got, tt.want) {
				t.Errorf("DataStore ListNodes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func IsStringsEqualWithoutOrder(one []string, two []string) bool {
	if len(one) != len(two) {
		return false
	}
	sort.Strings(one)
	sort.Strings(two)
	for i := 0; i < len(one); i++ {
		if one[i] != two[i] {
			return false
		}
	}
	return true
}
