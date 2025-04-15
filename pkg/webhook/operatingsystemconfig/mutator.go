// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package operatingsystemconfig

import (
	"context"
	"errors"
	"fmt"

	"github.com/Masterminds/semver/v3"
	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	extensionswebhookcontext "github.com/gardener/gardener/extensions/pkg/webhook/context"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/component/extensions/operatingsystemconfig/original/components/kubelet"
	"github.com/gardener/gardener/pkg/component/extensions/operatingsystemconfig/utils"
	"github.com/go-logr/logr"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type mutator struct {
	client             client.Client
	kubeletConfigCodec kubelet.ConfigCodec
	fciCodec           utils.FileContentInlineCodec
	logger             logr.Logger
}

// NewMutator creates a new osc mutator.
func NewMutator(
	mgr manager.Manager,
	kubeletConfigCodec kubelet.ConfigCodec,
	fciCodec utils.FileContentInlineCodec,
	logger logr.Logger,
) extensionswebhook.Mutator {
	return &mutator{
		client: mgr.GetClient(),

		kubeletConfigCodec: kubeletConfigCodec,
		fciCodec:           fciCodec,
		logger:             logger.WithName("mutator"),
	}
}

// Mutate mutates the given object.
func (m *mutator) Mutate(ctx context.Context, new, old client.Object) error {
	// If the object does have a deletion timestamp then we don't want to mutate anything.
	if new.GetDeletionTimestamp() != nil {
		return nil
	}

	gctx := extensionswebhookcontext.NewGardenContext(m.client, new)

	if x, ok := new.(*extensionsv1alpha1.OperatingSystemConfig); ok {
		var oldOSC *extensionsv1alpha1.OperatingSystemConfig
		if old != nil {
			var ok bool
			oldOSC, ok = old.(*extensionsv1alpha1.OperatingSystemConfig)
			if !ok {
				return errors.New("could not cast old object to extensionsv1alpha1.OperatingSystemConfig")
			}
		}

		if x.Spec.Purpose == extensionsv1alpha1.OperatingSystemConfigPurposeReconcile {
			extensionswebhook.LogMutation(m.logger, x.Kind, x.Namespace, x.Name)
			return m.mutateOperatingSystemConfigReconcile(ctx, gctx, x, oldOSC)
		}

		return nil
	}
	return nil
}

func (m *mutator) mutateOperatingSystemConfigReconcile(ctx context.Context, gctx extensionswebhookcontext.GardenContext, osc, oldOSC *extensionsv1alpha1.OperatingSystemConfig) error {
	cluster, err := gctx.GetCluster(ctx)
	if err != nil {
		return err
	}

	var cHostVersion *semver.Version
	if poolName, ok := osc.Labels[v1beta1constants.LabelWorkerPool]; ok {
		for _, worker := range cluster.Shoot.Spec.Provider.Workers {
			if worker.Name == poolName {
				if worker.Machine.Image == nil || worker.Machine.Image.Version == nil {
					return fmt.Errorf("could not obtain cHost version for worker pool from Shoot spec")
				}
				cHostVersion, err = semver.NewVersion(*worker.Machine.Image.Version)
				if err != nil {
					return err
				}
				break
			}
		}
	}

	if cHostVersion == nil {
		return fmt.Errorf("could not find matching worker pool in Shoot spec")
	}

	// Mutate kubelet configuration file, if present
	if content := getKubeletConfigFile(osc); content != nil {
		var kubeletConfig *kubeletconfigv1beta1.KubeletConfiguration

		if kubeletConfig, err = m.kubeletConfigCodec.Decode(content); err != nil {
			return fmt.Errorf("could not decode kubelet configuration: %w", err)
		}

		ensureKubeletConfiguration(m.logger, *cHostVersion, kubeletConfig)

		// Encode kubelet configuration into inline content
		var newContent *extensionsv1alpha1.FileContentInline
		if newContent, err = m.kubeletConfigCodec.Encode(kubeletConfig, content.Encoding); err != nil {
			return fmt.Errorf("could not encode kubelet configuration: %w", err)
		}

		*content = *newContent
	}

	// Mutate CRI configuration, if present
	if osc.Spec.CRIConfig != nil {
		ensureCRIConfig(logger, *cHostVersion, osc.Spec.CRIConfig)
	}

	return nil
}

func getKubeletConfigFile(osc *extensionsv1alpha1.OperatingSystemConfig) *extensionsv1alpha1.FileContentInline {
	return findFileWithPath(osc, v1beta1constants.OperatingSystemConfigFilePathKubeletConfig)
}

func findFileWithPath(osc *extensionsv1alpha1.OperatingSystemConfig, path string) *extensionsv1alpha1.FileContentInline {
	if osc != nil {
		if f := extensionswebhook.FileWithPath(osc.Spec.Files, path); f != nil {
			return f.Content.Inline
		}
	}

	return nil
}
