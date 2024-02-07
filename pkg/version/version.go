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

package version

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	GitCommit  = "unknown"
	GitSummary = "v0.0.0-master+$Format:%h$"
	BuildDate  = "1970-01-01T00:00:00Z"
	Version    = ""
)

func NewVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information and quit",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Version:%s, GitCommit:%s, GitSummary:%s, BuildDate:%s\n", Version, GitCommit, GitSummary, BuildDate)
			os.Exit(0)
		},
	}
}
