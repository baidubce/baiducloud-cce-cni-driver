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

package api

import (
	"os"
	"time"
)

const (
	// CCEGroupName is the cce's unix group name.
	CCEGroupName = "cce"
	// SocketFileMode is the default file mode for the sockets.
	SocketFileMode os.FileMode = 0660
	// ClientTimeout specifies timeout to be used by clients
	ClientTimeout = 90 * time.Second
)
