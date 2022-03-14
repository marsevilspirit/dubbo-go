/*
 *
 * Copyright 2021 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package client

import (
	"dubbo.apache.org/dubbo-go/v3/xds/client/bootstrap"
	"dubbo.apache.org/dubbo-go/v3/xds/client/controller"
	"dubbo.apache.org/dubbo-go/v3/xds/client/load"
	"dubbo.apache.org/dubbo-go/v3/xds/client/pubsub"
	"dubbo.apache.org/dubbo-go/v3/xds/client/resource"
	"dubbo.apache.org/dubbo-go/v3/xds/utils/grpclog"
)

type controllerInterface interface {
	AddWatch(resourceType resource.ResourceType, resourceName string)
	RemoveWatch(resourceType resource.ResourceType, resourceName string)
	ReportLoad(server string) (*load.Store, func())
	Close()
}

var newController = func(config *bootstrap.ServerConfig, pubsub *pubsub.Pubsub, validator resource.UpdateValidatorFunc, logger *grpclog.PrefixLogger) (controllerInterface, error) {
	return controller.New(config, pubsub, validator, logger)
}
