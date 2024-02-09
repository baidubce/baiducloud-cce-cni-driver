package eni

import "github.com/baidubce/bce-sdk-go/services/eni"

type ListEniResult struct {
	Eni         []Eni  `json:"enis"`
	Marker      string `json:"marker"`
	IsTruncated bool   `json:"isTruncated"`
	NextMarker  string `json:"nextMarker"`
	MaxKeys     int    `json:"maxKeys"`
}

type Eni struct {
	eni.Eni
	NetworkInterfaceTrafficMode string `json:"networkInterfaceTrafficMode"`
}
