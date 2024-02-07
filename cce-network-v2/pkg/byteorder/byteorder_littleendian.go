//go:build 386 || amd64 || arm || arm64 || mips64le || ppc64le || riscv64 || wasm

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

package byteorder

import (
	"encoding/binary"
	"math/bits"
)

var Native binary.ByteOrder = binary.LittleEndian

func HostToNetwork16(u uint16) uint16 { return bits.ReverseBytes16(u) }
func HostToNetwork32(u uint32) uint32 { return bits.ReverseBytes32(u) }
func NetworkToHost16(u uint16) uint16 { return bits.ReverseBytes16(u) }
func NetworkToHost32(u uint32) uint32 { return bits.ReverseBytes32(u) }
