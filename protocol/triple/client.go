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
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"
)

import (
	"github.com/dubbogo/gost/log/logger"

	"github.com/dustin/go-humanize"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"

	"golang.org/x/net/http2"
)

import (
	"dubbo.apache.org/dubbo-go/v3/common"
	"dubbo.apache.org/dubbo-go/v3/common/constant"
	"dubbo.apache.org/dubbo-go/v3/global"
	tri "dubbo.apache.org/dubbo-go/v3/protocol/triple/triple_protocol"
	dubbotls "dubbo.apache.org/dubbo-go/v3/tls"
)

const (
	httpPrefix  string = "http://"
	httpsPrefix string = "https://"
)

// ConnectionPoolConfig 连接池配置
type ConnectionPoolConfig struct {
	MaxConnections      int           `default:"100"`
	MaxIdleConnections  int           `default:"10"`
	IdleTimeout         time.Duration `default:"30s"`
	HealthCheckInterval time.Duration `default:"10s"`
	MaxRetries          int           `default:"3"`
	RetryDelay          time.Duration `default:"100ms"`
	EnableHealthCheck   bool          `default:"true"`
}

// PooledConnection 表示一个池化的连接
type PooledConnection struct {
	id         string
	client     *tri.Client
	httpClient *http.Client
	url        *common.URL
	lastUsed   time.Time
	createdAt  time.Time
	useCount   int64
	isActive   bool
	mu         sync.RWMutex
}

// DefaultConnectionPoolConfig 默认连接池配置
func DefaultConnectionPoolConfig() *ConnectionPoolConfig {
	return &ConnectionPoolConfig{
		MaxConnections:      100,
		MaxIdleConnections:  10,
		IdleTimeout:         30 * time.Second,
		HealthCheckInterval: 10 * time.Second,
		MaxRetries:          3,
		RetryDelay:          100 * time.Millisecond,
		EnableHealthCheck:   true,
	}
}

// PooledConnection 方法实现

func (pc *PooledConnection) isHealthy() bool {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	return pc.isActive || time.Since(pc.lastUsed) <= 30*time.Second
}

func (pc *PooledConnection) markActive() {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.isActive = true
	pc.lastUsed = time.Now()
}

func (pc *PooledConnection) GetClient() *tri.Client {
	return pc.client
}

func (pc *PooledConnection) GetHttpClient() *http.Client {
	return pc.httpClient
}

func (pc *PooledConnection) GetURL() *common.URL {
	return pc.url
}

func (pc *PooledConnection) GetLastUsed() time.Time {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	return pc.lastUsed
}

func (pc *PooledConnection) GetUseCount() int64 {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	return pc.useCount
}

// clientManager wraps triple clients and is responsible for find concrete triple client to invoke
// callUnary, callClientStream, callServerStream, callBidiStream.
// A Reference has a clientManager.
type clientManager struct {
	isIDL bool
	// triple_protocol clients, key is method name
	triClients map[string]*tri.Client
	// HTTP client for connection pooling
	httpClient *http.Client
	// URL for this client manager
	url *common.URL
	// Connection pool configuration
	poolConfig *ConnectionPoolConfig
	// Connection pool for HTTP connections
	connectionPool map[string]*PooledConnection
	poolMutex      sync.RWMutex
}

// TODO: code a triple client between clientManager and triple_protocol client
// TODO: write a NewClient for triple client

func (cm *clientManager) getClient(method string) (*tri.Client, error) {
	triClient, ok := cm.triClients[method]
	if !ok {
		return nil, fmt.Errorf("missing triple client for method: %s", method)
	}
	return triClient, nil
}

func (cm *clientManager) callUnary(ctx context.Context, method string, req, resp any) error {
	// 从连接池获取连接
	conn, err := cm.getPooledConnection(method, cm.getURL())
	if err != nil {
		return err
	}

	// 确保连接在使用后被释放
	defer cm.releasePooledConnection(conn)

	triReq := tri.NewRequest(req)
	triResp := tri.NewResponse(resp)
	if err := conn.GetClient().CallUnary(ctx, triReq, triResp); err != nil {
		return err
	}
	return nil
}

func (cm *clientManager) callClientStream(ctx context.Context, method string) (any, error) {
	// 从连接池获取连接
	conn, err := cm.getPooledConnection(method, cm.getURL())
	if err != nil {
		return nil, err
	}

	// 注意：对于流式调用，连接需要在流关闭时释放
	// 这里暂时不释放，由调用方负责释放
	stream, err := conn.GetClient().CallClientStream(ctx)
	if err != nil {
		cm.releasePooledConnection(conn)
		return nil, err
	}
	return stream, nil
}

func (cm *clientManager) callServerStream(ctx context.Context, method string, req any) (any, error) {
	// 从连接池获取连接
	conn, err := cm.getPooledConnection(method, cm.getURL())
	if err != nil {
		return nil, err
	}

	// 注意：对于流式调用，连接需要在流关闭时释放
	// 这里暂时不释放，由调用方负责释放
	triReq := tri.NewRequest(req)
	stream, err := conn.GetClient().CallServerStream(ctx, triReq)
	if err != nil {
		cm.releasePooledConnection(conn)
		return nil, err
	}
	return stream, nil
}

func (cm *clientManager) callBidiStream(ctx context.Context, method string) (any, error) {
	// 从连接池获取连接
	conn, err := cm.getPooledConnection(method, cm.getURL())
	if err != nil {
		return nil, err
	}

	// 注意：对于流式调用，连接需要在流关闭时释放
	// 这里暂时不释放，由调用方负责释放
	stream, err := conn.GetClient().CallBidiStream(ctx)
	if err != nil {
		cm.releasePooledConnection(conn)
		return nil, err
	}
	return stream, nil
}

func (cm *clientManager) close() error {
	// 关闭所有池化连接
	cm.poolMutex.Lock()
	defer cm.poolMutex.Unlock()

	for _, conn := range cm.connectionPool {
		conn.mu.Lock()
		conn.isActive = false
		conn.mu.Unlock()
	}
	cm.connectionPool = make(map[string]*PooledConnection)
	return nil
}

// getPooledConnection 从连接池获取连接
func (cm *clientManager) getPooledConnection(method string, url *common.URL) (*PooledConnection, error) {
	connectionKey := cm.getConnectionKey(url)

	cm.poolMutex.RLock()
	conn, exists := cm.connectionPool[connectionKey]
	cm.poolMutex.RUnlock()

	if exists && conn.isHealthy() {
		conn.markActive()
		return conn, nil
	}

	// 如果连接不存在或不健康，创建新连接
	return cm.createPooledConnection(method, url, connectionKey)
}

// createPooledConnection 创建新的池化连接
func (cm *clientManager) createPooledConnection(method string, url *common.URL, connectionKey string) (*PooledConnection, error) {
	cm.poolMutex.Lock()
	defer cm.poolMutex.Unlock()

	// 检查连接数限制
	if len(cm.connectionPool) >= cm.poolConfig.MaxConnections {
		// 尝试清理不健康的连接
		cm.cleanupUnhealthyConnections()
		if len(cm.connectionPool) >= cm.poolConfig.MaxConnections {
			return nil, fmt.Errorf("connection pool is full, max connections: %d", cm.poolConfig.MaxConnections)
		}
	}

	// 创建新的连接
	conn, err := cm.newPooledConnection(method, url)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection: %w", err)
	}

	cm.connectionPool[connectionKey] = conn
	logger.Debugf("Created new pooled connection: %s", connectionKey)
	return conn, nil
}

// newPooledConnection 创建单个池化连接
func (cm *clientManager) newPooledConnection(method string, url *common.URL) (*PooledConnection, error) {
	// 获取指定方法的客户端
	client, err := cm.getClient(method)
	if err != nil {
		return nil, err
	}

	// 创建连接对象
	conn := &PooledConnection{
		id:         fmt.Sprintf("%s-%s-%d", url.Location, method, time.Now().UnixNano()),
		client:     client,
		httpClient: cm.httpClient,
		url:        url,
		createdAt:  time.Now(),
		lastUsed:   time.Now(),
		isActive:   true,
	}

	return conn, nil
}

// releasePooledConnection 释放连接回连接池
func (cm *clientManager) releasePooledConnection(conn *PooledConnection) {
	if conn == nil {
		return
	}

	conn.mu.Lock()
	defer conn.mu.Unlock()

	if !conn.isActive {
		return
	}

	conn.isActive = false
	conn.lastUsed = time.Now()
	conn.useCount++

	logger.Debugf("Released pooled connection: %s", conn.id)
}

// getConnectionKey 生成连接键
func (cm *clientManager) getConnectionKey(url *common.URL) string {
	return fmt.Sprintf("%s://%s%s", url.Protocol, url.Location, url.Path)
}

// cleanupUnhealthyConnections 清理不健康的连接
func (cm *clientManager) cleanupUnhealthyConnections() {
	now := time.Now()
	toRemove := make([]string, 0)

	for key, conn := range cm.connectionPool {
		conn.mu.RLock()
		shouldRemove := !conn.isActive && now.Sub(conn.lastUsed) > cm.poolConfig.IdleTimeout
		conn.mu.RUnlock()

		if shouldRemove {
			toRemove = append(toRemove, key)
		}
	}

	for _, key := range toRemove {
		delete(cm.connectionPool, key)
		logger.Debugf("Removed unhealthy connection: %s", key)
	}
}

// GetPoolStats 获取连接池统计信息
func (cm *clientManager) GetPoolStats() map[string]interface{} {
	cm.poolMutex.RLock()
	defer cm.poolMutex.RUnlock()

	totalConnections := len(cm.connectionPool)
	activeConnections := 0
	idleConnections := 0

	for _, conn := range cm.connectionPool {
		conn.mu.RLock()
		if conn.isActive {
			activeConnections++
		} else {
			idleConnections++
		}
		conn.mu.RUnlock()
	}

	return map[string]interface{}{
		"total_connections":  totalConnections,
		"active_connections": activeConnections,
		"idle_connections":   idleConnections,
		"pool_size":          totalConnections,
		"max_connections":    cm.poolConfig.MaxConnections,
		"max_idle":           cm.poolConfig.MaxIdleConnections,
	}
}

// getURL 获取URL
func (cm *clientManager) getURL() *common.URL {
	return cm.url
}

// newClientManager extracts configurations from url and builds clientManager
func newClientManager(url *common.URL) (*clientManager, error) {
	var cliOpts []tri.ClientOption
	var isIDL bool

	// set serialization
	serialization := url.GetParam(constant.SerializationKey, constant.ProtobufSerialization)
	switch serialization {
	case constant.ProtobufSerialization:
		isIDL = true
	case constant.JSONSerialization:
		isIDL = true
		cliOpts = append(cliOpts, tri.WithProtoJSON())
	case constant.Hessian2Serialization:
		cliOpts = append(cliOpts, tri.WithHessian2())
	case constant.MsgpackSerialization:
		cliOpts = append(cliOpts, tri.WithMsgPack())
	default:
		panic(fmt.Sprintf("Unsupported serialization: %s", serialization))
	}

	// set timeout
	timeout := url.GetParamDuration(constant.TimeoutKey, "")
	cliOpts = append(cliOpts, tri.WithTimeout(timeout))

	// set service group and version
	group := url.GetParam(constant.GroupKey, "")
	version := url.GetParam(constant.VersionKey, "")
	cliOpts = append(cliOpts, tri.WithGroup(group), tri.WithVersion(version))

	// todo(DMwangnima): support opentracing

	// handle tls
	var (
		tlsFlag bool
		tlsConf *global.TLSConfig
		cfg     *tls.Config
		err     error
	)

	tlsConfRaw, ok := url.GetAttribute(constant.TLSConfigKey)
	if ok {
		tlsConf, ok = tlsConfRaw.(*global.TLSConfig)
		if !ok {
			return nil, errors.New("TRIPLE clientManager initialized the TLSConfig configuration failed")
		}
	}
	if dubbotls.IsClientTLSValid(tlsConf) {
		cfg, err = dubbotls.GetClientTlSConfig(tlsConf)
		if err != nil {
			return nil, err
		}
		if cfg != nil {
			logger.Infof("TRIPLE clientManager initialized the TLSConfig configuration")
			tlsFlag = true
		}
	}

	var tripleConf *global.TripleConfig

	tripleConfRaw, ok := url.GetAttribute(constant.TripleConfigKey)
	if ok {
		tripleConf = tripleConfRaw.(*global.TripleConfig)
	}

	// handle keepalive options
	cliKeepAliveOpts, keepAliveInterval, keepAliveTimeout, genKeepAliveOptsErr := genKeepAliveOptions(url, tripleConf)
	if genKeepAliveOptsErr != nil {
		logger.Errorf("genKeepAliveOpts err: %v", genKeepAliveOptsErr)
		return nil, genKeepAliveOptsErr
	}
	cliOpts = append(cliOpts, cliKeepAliveOpts...)

	// handle http transport of triple protocol
	var transport http.RoundTripper

	var callProtocol string
	if tripleConf != nil && tripleConf.Http3 != nil && tripleConf.Http3.Enable {
		callProtocol = constant.CallHTTP3
	} else {
		// HTTP default type is HTTP/2.
		callProtocol = constant.CallHTTP2
	}

	switch callProtocol {
	// This case might be for backward compatibility,
	// it's not useful for the Triple protocol, HTTP/1 lacks trailer functionality.
	// Triple protocol only supports HTTP/2 and HTTP/3.
	case constant.CallHTTP:
		transport = &http.Transport{
			TLSClientConfig: cfg,
		}
		cliOpts = append(cliOpts, tri.WithTriple())
	case constant.CallHTTP2:
		// TODO: Enrich the http2 transport config for triple protocol.
		if tlsFlag {
			transport = &http2.Transport{
				TLSClientConfig: cfg,
				ReadIdleTimeout: keepAliveInterval,
				PingTimeout:     keepAliveTimeout,
			}
		} else {
			transport = &http2.Transport{
				DialTLSContext: func(_ context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
					return net.Dial(network, addr)
				},
				AllowHTTP:       true,
				ReadIdleTimeout: keepAliveInterval,
				PingTimeout:     keepAliveTimeout,
			}
		}
	case constant.CallHTTP3:
		if !tlsFlag {
			return nil, fmt.Errorf("TRIPLE http3 client must have TLS config, but TLS config is nil")
		}

		// TODO: Enrich the http3 transport config for triple protocol.
		transport = &http3.Transport{
			TLSClientConfig: cfg,
			QUICConfig: &quic.Config{
				// ref: https://quic-go.net/docs/quic/connection/#keeping-a-connection-alive
				KeepAlivePeriod: keepAliveInterval,
				// ref: https://quic-go.net/docs/quic/connection/#idle-timeout
				MaxIdleTimeout: keepAliveTimeout,
			},
		}

		logger.Infof("Triple http3 client transport init successfully")
	default:
		return nil, fmt.Errorf("unsupported http protocol: %s", callProtocol)
	}

	httpClient := &http.Client{
		Transport: transport,
	}

	var baseTriURL string
	baseTriURL = strings.TrimPrefix(url.Location, httpPrefix)
	baseTriURL = strings.TrimPrefix(baseTriURL, httpsPrefix)
	if tlsFlag {
		baseTriURL = httpsPrefix + baseTriURL
	} else {
		baseTriURL = httpPrefix + baseTriURL
	}

	triClients := make(map[string]*tri.Client)

	if len(url.Methods) != 0 {
		for _, method := range url.Methods {
			triURL, err := joinPath(baseTriURL, url.Interface(), method)
			if err != nil {
				return nil, fmt.Errorf("JoinPath failed for base %s, interface %s, method %s", baseTriURL, url.Interface(), method)
			}
			triClient := tri.NewClient(httpClient, triURL, cliOpts...)
			triClients[method] = triClient
		}
	} else {
		// This branch is for the non-IDL mode, where we pass in the service solely
		// for the purpose of using reflection to obtain all methods of the service.
		// There might be potential for optimization in this area later on.
		service, ok := url.GetAttribute(constant.RpcServiceKey)
		if !ok {
			return nil, fmt.Errorf("triple clientmanager can't get methods")
		}

		serviceType := reflect.TypeOf(service)
		for i := range serviceType.NumMethod() {
			methodName := serviceType.Method(i).Name
			triURL, err := joinPath(baseTriURL, url.Interface(), methodName)
			if err != nil {
				return nil, fmt.Errorf("JoinPath failed for base %s, interface %s, method %s", baseTriURL, url.Interface(), methodName)
			}
			triClient := tri.NewClient(httpClient, triURL, cliOpts...)
			triClients[methodName] = triClient
		}
	}

	// 创建连接池配置
	poolConfig := DefaultConnectionPoolConfig()

	// 从URL参数中读取连接池配置
	if maxConn := url.GetParamInt("max_connections", 0); maxConn > 0 {
		poolConfig.MaxConnections = int(maxConn)
	}
	if maxIdle := url.GetParamInt("max_idle_connections", 0); maxIdle > 0 {
		poolConfig.MaxIdleConnections = int(maxIdle)
	}
	if idleTimeout := url.GetParamDuration("idle_timeout", ""); idleTimeout > 0 {
		poolConfig.IdleTimeout = idleTimeout
	}
	if healthCheckInterval := url.GetParamDuration("health_check_interval", ""); healthCheckInterval > 0 {
		poolConfig.HealthCheckInterval = healthCheckInterval
	}

	return &clientManager{
		isIDL:          isIDL,
		triClients:     triClients,
		httpClient:     httpClient,
		url:            url,
		poolConfig:     poolConfig,
		connectionPool: make(map[string]*PooledConnection),
	}, nil
}

func genKeepAliveOptions(url *common.URL, tripleConf *global.TripleConfig) ([]tri.ClientOption, time.Duration, time.Duration, error) {
	var cliKeepAliveOpts []tri.ClientOption

	// set max send and recv msg size
	maxCallRecvMsgSize := constant.DefaultMaxCallRecvMsgSize
	if recvMsgSize, err := humanize.ParseBytes(url.GetParam(constant.MaxCallRecvMsgSize, "")); err == nil && recvMsgSize > 0 {
		maxCallRecvMsgSize = int(recvMsgSize)
	}
	cliKeepAliveOpts = append(cliKeepAliveOpts, tri.WithReadMaxBytes(maxCallRecvMsgSize))
	maxCallSendMsgSize := constant.DefaultMaxCallSendMsgSize
	if sendMsgSize, err := humanize.ParseBytes(url.GetParam(constant.MaxCallSendMsgSize, "")); err == nil && sendMsgSize > 0 {
		maxCallSendMsgSize = int(sendMsgSize)
	}
	cliKeepAliveOpts = append(cliKeepAliveOpts, tri.WithSendMaxBytes(maxCallSendMsgSize))

	// set keepalive interval and keepalive timeout
	// Deprecated：use tripleconfig
	// TODO: remove KeepAliveInterval and KeepAliveInterval in version 4.0.0
	keepAliveInterval := url.GetParamDuration(constant.KeepAliveInterval, constant.DefaultKeepAliveInterval)
	keepAliveTimeout := url.GetParamDuration(constant.KeepAliveTimeout, constant.DefaultKeepAliveTimeout)

	if tripleConf == nil {
		return cliKeepAliveOpts, keepAliveInterval, keepAliveTimeout, nil
	}

	var parseErr error

	if tripleConf.KeepAliveInterval != "" {
		keepAliveInterval, parseErr = time.ParseDuration(tripleConf.KeepAliveInterval)
		if parseErr != nil {
			return nil, 0, 0, parseErr
		}
	}
	if tripleConf.KeepAliveTimeout != "" {
		keepAliveTimeout, parseErr = time.ParseDuration(tripleConf.KeepAliveTimeout)
		if parseErr != nil {
			return nil, 0, 0, parseErr
		}
	}

	return cliKeepAliveOpts, keepAliveInterval, keepAliveTimeout, nil
}
