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

package active

import (
	"context"
	"errors"
	"strconv"
	"testing"
)

import (
	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/assert"
)

import (
	"dubbo.apache.org/dubbo-go/v3/common"
	"dubbo.apache.org/dubbo-go/v3/protocol/base"
	"dubbo.apache.org/dubbo-go/v3/protocol/invocation"
	"dubbo.apache.org/dubbo-go/v3/protocol/mock"
	"dubbo.apache.org/dubbo-go/v3/protocol/result"
)

func TestFilterInvoke(t *testing.T) {
	invoc := invocation.NewRPCInvocation("test", []any{"OK"}, make(map[string]any))
	url, _ := common.NewURL("dubbo://192.168.10.10:20000/com.ikurento.user.UserProvider")
	filter := activeFilter{}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	invoker := mock.NewMockInvoker(ctrl)
	invoker.EXPECT().Invoke(gomock.Any(), gomock.Any()).Return(nil)
	invoker.EXPECT().GetURL().Return(url).Times(1)
	filter.Invoke(context.Background(), invoker, invoc)
	assert.True(t, invoc.GetAttachmentWithDefaultValue(dubboInvokeStartTime, "") != "")
}

func TestFilterOnResponse(t *testing.T) {
	c := base.CurrentTimeMillis()
	elapsed := 100
	invoc := invocation.NewRPCInvocation("test", []any{"OK"}, map[string]any{
		dubboInvokeStartTime: strconv.FormatInt(c-int64(elapsed), 10),
	})
	url, _ := common.NewURL("dubbo://192.168.10.10:20000/com.ikurento.user.UserProvider")
	filter := activeFilter{}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	invoker := mock.NewMockInvoker(ctrl)
	invoker.EXPECT().GetURL().Return(url).Times(1)
	result := &result.RPCResult{
		Err: errors.New("test"),
	}
	filter.OnResponse(context.TODO(), result, invoker, invoc)
	methodStatus := base.GetMethodStatus(url, "test")
	urlStatus := base.GetURLStatus(url)

	assert.Equal(t, int32(1), methodStatus.GetTotal())
	assert.Equal(t, int32(1), urlStatus.GetTotal())
	assert.Equal(t, int32(1), methodStatus.GetFailed())
	assert.Equal(t, int32(1), urlStatus.GetFailed())
	assert.Equal(t, int32(1), methodStatus.GetSuccessiveRequestFailureCount())
	assert.Equal(t, int32(1), urlStatus.GetSuccessiveRequestFailureCount())
	assert.True(t, methodStatus.GetFailedElapsed() >= int64(elapsed))
	assert.True(t, urlStatus.GetFailedElapsed() >= int64(elapsed))
	assert.True(t, urlStatus.GetLastRequestFailedTimestamp() != int64(0))
	assert.True(t, methodStatus.GetLastRequestFailedTimestamp() != int64(0))
}
