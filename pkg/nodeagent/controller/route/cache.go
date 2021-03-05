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

package route

import (
	"sync"
)

type cachedStaticRoute struct {
	Dst     string
	Gateway string
}

type StaticRouteCache struct {
	lock     sync.Mutex
	routeMap map[string]*cachedStaticRoute
}

func (c *StaticRouteCache) get(key string) (*cachedStaticRoute, bool) {
	c.lock.Lock()
	defer c.lock.Unlock()

	rt, ok := c.routeMap[key]
	return rt, ok
}

func (c *StaticRouteCache) set(key string, rt *cachedStaticRoute) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.routeMap[key] = rt
}

func (c *StaticRouteCache) delete(key string) {
	c.lock.Lock()
	defer c.lock.Unlock()

	delete(c.routeMap, key)
}
