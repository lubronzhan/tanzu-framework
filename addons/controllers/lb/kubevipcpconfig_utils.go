// Copyright 2022 VMware, Inc. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"fmt"
	"strings"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterapiv1beta1 "sigs.k8s.io/cluster-api/api/v1beta1"
	clusterapiutil "sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/vmware-tanzu/tanzu-framework/addons/pkg/constants"
	lbv1alpha1 "github.com/vmware-tanzu/tanzu-framework/apis/addonconfigs/lb/v1alpha1"
)

// ClusterToKubevipCPConfig returns a list of Requests with KubevipCPConfig ObjectKey based on Cluster events
func (r *KubevipCPConfigReconciler) ClusterToKubevipCPConfig(o client.Object) []ctrl.Request {
	cluster, ok := o.(*clusterapiv1beta1.Cluster)
	if !ok {
		r.Log.Error(errors.New("invalid type"),
			"Expected to receive Cluster resource",
			"actualType", fmt.Sprintf("%T", o))
		return nil
	}

	r.Log.V(4).Info("Mapping Cluster to KubevipCPConfig")

	cs := &lbv1alpha1.KubevipCPConfigList{}
	_ = r.List(context.Background(), cs)

	requests := []ctrl.Request{}
	for i := 0; i < len(cs.Items); i++ {
		config := &cs.Items[i]
		if config.Namespace == cluster.Namespace {
			// avoid enqueuing reconcile requests for template KubevipCPConfig CRs in event handler of Cluster CR
			if _, ok := config.Annotations[constants.TKGAnnotationTemplateConfig]; ok && config.Namespace == r.Config.SystemNamespace {
				continue
			}

			// corresponding KubevipCPConfig should have following ownerRef
			ownerReference := metav1.OwnerReference{
				APIVersion: clusterapiv1beta1.GroupVersion.String(),
				Kind:       cluster.Kind,
				Name:       cluster.Name,
				UID:        cluster.UID,
			}
			if clusterapiutil.HasOwnerRef(config.OwnerReferences, ownerReference) {
				r.Log.V(4).Info("Adding KubevipCPConfig for reconciliation",
					constants.NamespaceLogKey, config.Namespace, constants.NameLogKey, config.Name)

				requests = append(requests, ctrl.Request{
					NamespacedName: clusterapiutil.ObjectKey(config),
				})
			}
		}
	}

	return requests
}

// mapkubevipCPConfigToDataValuesNonParavirtual generates CPI data values for non-paravirtual modes
func (r *KubevipCPConfigReconciler) mapKubevipCPConfigToDataValues( // nolint
	ctx context.Context,
	kubevipCPConfig *lbv1alpha1.KubevipCPConfig, cluster *clusterapiv1beta1.Cluster) (KubevipCloudProviderDataValues, error,
) { // nolint:whitespace
	// allow API user to override the derived values if he/she specified fields in the KubevipCPConfig
	dataValue := &KubevipCloudProviderDataValues{}
	config := kubevipCPConfig.Spec
	dataValue.LoadbalancerCIDRs = tryParseString(dataValue.LoadbalancerCIDRs, config.KubevipLoadbalancerCIDRs)
	dataValue.LoadbalancerIPRanges = tryParseString(dataValue.LoadbalancerIPRanges, config.KubevipLoadbalancerIPRanges)

	return *dataValue, nil
}

// getOwnerCluster verifies that the KubevipCPConfig has a cluster as its owner reference,
// and returns the cluster. It tries to read the cluster name from the KubevipCPConfig's owner reference objects.
// If not there, we assume the owner cluster and KubevipCPConfig always has the same name.
func (r *KubevipCPConfigReconciler) getOwnerCluster(ctx context.Context, kubevipCPConfig *lbv1alpha1.KubevipCPConfig) (*clusterapiv1beta1.Cluster, error) {
	cluster := &clusterapiv1beta1.Cluster{}
	clusterName := ""

	// retrieve the owner cluster for the KubevipCPConfig object
	for _, ownerRef := range kubevipCPConfig.GetOwnerReferences() {
		if strings.EqualFold(ownerRef.Kind, constants.ClusterKind) {
			clusterName = ownerRef.Name
			break
		}
	}

	if len(clusterName) == 0 {
		r.Log.Error(errors.New("ownerRef not found"), fmt.Sprintf("Cluster OwnerRef for KubevipCPConfig '%s/%s' not found", kubevipCPConfig.Namespace, kubevipCPConfig.Name))
		return nil, nil
	}

	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: kubevipCPConfig.Namespace, Name: clusterName}, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			r.Log.Error(err, fmt.Sprintf("Cluster resource '%s/%s' not found", kubevipCPConfig.Namespace, clusterName))
			return nil, err
		}
		r.Log.Error(err, fmt.Sprintf("Unable to fetch cluster '%s/%s'", kubevipCPConfig.Namespace, clusterName))
		return nil, err
	}
	r.Log.Info(fmt.Sprintf("Cluster resource '%s/%s' is successfully found", kubevipCPConfig.Namespace, clusterName))
	return cluster, nil
}

// tryParseString tries to convert a string pointer and return its value, if not nil
func tryParseString(src string, sub *string) string {
	if sub != nil {
		return *sub
	}
	return src
}
