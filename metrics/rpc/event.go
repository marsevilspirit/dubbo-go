/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package rpc

import (
	"time"
)

import (
	"dubbo.apache.org/dubbo-go/v3/common/constant"
	"dubbo.apache.org/dubbo-go/v3/metrics"
	"dubbo.apache.org/dubbo-go/v3/protocol/base"
	"dubbo.apache.org/dubbo-go/v3/protocol/result"
)

// metricsEvent is the event defined for rpc metrics
type metricsEvent struct {
	name       metricsName
	invoker    base.Invoker
	invocation base.Invocation
	costTime   time.Duration
	result     result.Result
}

// Type returns the type of the event, it is used for metrics bus to dispatch the event to rpc collector
func (m metricsEvent) Type() string {
	return constant.MetricsRpc
}

type metricsName uint8

const (
	BeforeInvoke metricsName = iota
	AfterInvoke
)

func NewBeforeInvokeEvent(invoker base.Invoker, invocation base.Invocation) metrics.MetricsEvent {
	return &metricsEvent{
		name:       BeforeInvoke,
		invoker:    invoker,
		invocation: invocation,
	}
}

func NewAfterInvokeEvent(invoker base.Invoker, invocation base.Invocation, costTime time.Duration, result result.Result) metrics.MetricsEvent {
	return &metricsEvent{
		name:       AfterInvoke,
		invoker:    invoker,
		invocation: invocation,
		costTime:   costTime,
		result:     result,
	}
}
