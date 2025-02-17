// Copyright 2025 SAP SE
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"errors"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func DefaultKubernetesCacheOpts() cache.Options {
	return cache.Options{
		DefaultTransform: cache.TransformStripManagedFields(),
		ByObject: map[client.Object]cache.ByObject{
			&corev1.Pod{}: {
				Transform: func(obj any) (any, error) {
					pod, ok := obj.(*corev1.Pod)
					if !ok {
						return nil, errors.New("received no pod object to pod transform func")
					}
					pod.ManagedFields = nil
					pod.Spec.Containers = nil
					pod.Spec.Volumes = nil
					return pod, nil
				},
			},
		},
	}
}
