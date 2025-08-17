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

package triple

import (
	"testing"
	"time"
)

import (
	"github.com/stretchr/testify/assert"
)

import (
	"dubbo.apache.org/dubbo-go/v3/common"
)

func TestClientManagerWithConnectionPool(t *testing.T) {
	// 创建测试URL
	url := common.NewURLWithOptions(
		common.WithProtocol("tri"),
		common.WithLocation("localhost:8080"),
		common.WithPath("test.service"),
		common.WithParamsValue("max_connections", "5"),
		common.WithParamsValue("max_idle_connections", "2"),
		common.WithParamsValue("idle_timeout", "1s"),
	)

	// 创建clientManager
	cm, err := newClientManager(url)
	assert.NoError(t, err)
	assert.NotNil(t, cm)
	defer cm.close()

	// 测试连接池配置
	assert.Equal(t, 5, cm.poolConfig.MaxConnections)
	assert.Equal(t, 2, cm.poolConfig.MaxIdleConnections)
	assert.Equal(t, time.Second, cm.poolConfig.IdleTimeout)

	// 测试获取连接池统计信息
	stats := cm.GetPoolStats()
	assert.Equal(t, 0, stats["total_connections"])
	assert.Equal(t, 0, stats["active_connections"])
	assert.Equal(t, 0, stats["idle_connections"])
	assert.Equal(t, 5, stats["max_connections"])
	assert.Equal(t, 2, stats["max_idle"])
}

func TestPooledConnection(t *testing.T) {
	// 创建测试连接
	conn := &PooledConnection{
		id:        "test-conn",
		createdAt: time.Now(),
		lastUsed:  time.Now(),
		isActive:  false,
	}

	// 测试连接健康状态
	assert.True(t, conn.isHealthy())

	// 测试标记为活跃
	conn.markActive()
	assert.True(t, conn.isActive)

	// 测试使用计数
	assert.Equal(t, int64(0), conn.GetUseCount())

	// 测试最后使用时间
	lastUsed := conn.GetLastUsed()
	assert.True(t, time.Since(lastUsed) < time.Second)
}

func TestConnectionPoolConfig(t *testing.T) {
	// 测试默认配置
	config := DefaultConnectionPoolConfig()
	assert.Equal(t, 100, config.MaxConnections)
	assert.Equal(t, 10, config.MaxIdleConnections)
	assert.Equal(t, 30*time.Second, config.IdleTimeout)
	assert.Equal(t, 10*time.Second, config.HealthCheckInterval)
	assert.Equal(t, 3, config.MaxRetries)
	assert.Equal(t, 100*time.Millisecond, config.RetryDelay)
	assert.True(t, config.EnableHealthCheck)
}

func TestClientManagerURLParsing(t *testing.T) {
	// 测试URL参数解析
	url := common.NewURLWithOptions(
		common.WithProtocol("tri"),
		common.WithLocation("localhost:8080"),
		common.WithPath("test.service"),
		common.WithParamsValue("max_connections", "50"),
		common.WithParamsValue("max_idle_connections", "5"),
		common.WithParamsValue("idle_timeout", "5s"),
		common.WithParamsValue("health_check_interval", "2s"),
	)

	cm, err := newClientManager(url)
	assert.NoError(t, err)
	assert.NotNil(t, cm)
	defer cm.close()

	// 验证参数是否正确解析
	assert.Equal(t, 50, cm.poolConfig.MaxConnections)
	assert.Equal(t, 5, cm.poolConfig.MaxIdleConnections)
	assert.Equal(t, 5*time.Second, cm.poolConfig.IdleTimeout)
	assert.Equal(t, 2*time.Second, cm.poolConfig.HealthCheckInterval)
}

func TestConnectionKeyGeneration(t *testing.T) {
	url := common.NewURLWithOptions(
		common.WithProtocol("tri"),
		common.WithLocation("localhost:8080"),
		common.WithPath("test.service"),
	)

	cm, err := newClientManager(url)
	assert.NoError(t, err)
	defer cm.close()

	// 测试连接键生成
	key := cm.getConnectionKey(url)
	expectedKey := "tri://localhost:8080/test.service"
	assert.Equal(t, expectedKey, key)
}
