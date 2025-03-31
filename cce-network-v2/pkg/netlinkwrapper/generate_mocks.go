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

package netlinkwrapper

//go:generate go run github.com/golang/mock/mockgen -destination mocks/netlinkwrapper_mocks.go -copyright_file ../../scripts/copyright.txt . NetLink
//go:generate go run github.com/golang/mock/mockgen -destination mock_netlink/link_mocks.go -copyright_file ../../scripts/copyright.txt github.com/vishvananda/netlink Link
