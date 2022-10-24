// Copyright 2022 VMware, Inc. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"fmt"
	"os"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterapiv1beta1 "sigs.k8s.io/cluster-api/api/v1beta1"
	clusterapiutil "sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/vmware-tanzu/tanzu-framework/addons/pkg/constants"
	"github.com/vmware-tanzu/tanzu-framework/addons/test/testutil"
	lbv1alpha1 "github.com/vmware-tanzu/tanzu-framework/apis/addonconfigs/lb/v1alpha1"
)

var _ = Describe("KubevipCPConfig Reconciler", func() {
	var (
		key                     client.ObjectKey
		clusterName             string
		clusterResourceFilePath string
	)

	JustBeforeEach(func() {
		By("Creating cluster and KubevipCPConfig resources")
		key = client.ObjectKey{
			Namespace: clusterNamespace,
			Name:      clusterName,
		}
		f, err := os.Open(clusterResourceFilePath)
		Expect(err).ToNot(HaveOccurred())
		defer f.Close()
		err = testutil.CreateResources(f, cfg, dynamicClient)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		By("Deleting cluster and KubevipCPConfig resources")
		for _, filePath := range []string{clusterResourceFilePath} {
			f, err := os.Open(filePath)
			Expect(err).ToNot(HaveOccurred())
			if err = testutil.DeleteResources(f, cfg, dynamicClient, true); !apierrors.IsNotFound(err) {
				// namespace has been explicitly deleted using testutil.DeleteNamespace
				// ignore its NotFound error here
				Expect(err).ToNot(HaveOccurred())
			}
			Expect(f.Close()).ToNot(HaveOccurred())
		}
	})

	Context("reconcile KubevipCPConfig manifests", func() {
		BeforeEach(func() {
			clusterName = "test-cluster-kvcp"
			clusterResourceFilePath = "testdata/test-kubevip-cloudprovider-config.yaml"
		})

		It("Should reconcile KubevipCPConfig and create data values secret for KubevipCPConfig on management cluster", func() {
			cluster := &clusterapiv1beta1.Cluster{}
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, key, cluster); err != nil {
					return false
				}
				return true
			}, waitTimeout, pollingInterval).Should(BeTrue())

			// the kvcp config object should be deployed
			config := &lbv1alpha1.KubevipCPConfig{}
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, key, config); err != nil {
					return false
				}

				if len(config.OwnerReferences) > 0 {
					return false
				}

				Expect(len(config.OwnerReferences)).Should(Equal(0))
				return true
			}, waitTimeout, pollingInterval).Should(BeTrue())

			By("patching kubevip cloudprovider with ownerRef as ClusterBootstrapController would do")
			// patch the KubevipCPConfig with ownerRef
			patchedKubevipCPConfig := config.DeepCopy()
			ownerRef := metav1.OwnerReference{
				APIVersion: clusterapiv1beta1.GroupVersion.String(),
				Kind:       cluster.Kind,
				Name:       cluster.Name,
				UID:        cluster.UID,
			}

			ownerRef.Kind = "Cluster"
			patchedKubevipCPConfig.OwnerReferences = clusterapiutil.EnsureOwnerRef(patchedKubevipCPConfig.OwnerReferences, ownerRef)
			Expect(k8sClient.Patch(ctx, patchedKubevipCPConfig, client.MergeFrom(config))).ShouldNot(HaveOccurred())

			// the data values secret should be generated
			secret := &v1.Secret{}
			Eventually(func() bool {
				secretKey := client.ObjectKey{
					Namespace: clusterNamespace,
					Name:      fmt.Sprintf("%s-%s-data-values", clusterName, constants.KubevipCloudProviderAddonName),
				}
				if err := k8sClient.Get(ctx, secretKey, secret); err != nil {
					return false
				}
				secretData := string(secret.Data["values.yaml"])
				Expect(len(secretData)).ShouldNot(BeZero())
				Expect(strings.Contains(secretData, "kubevipCloudProvider:")).Should(BeTrue())
				Expect(strings.Contains(secretData, "loadbalancerCIDRs:")).Should(BeTrue())
				Expect(strings.Contains(secretData, "loadbalancerIPRanges:")).Should(BeTrue())

				return true
			}, waitTimeout, pollingInterval).Should(BeTrue())

			// eventually the secret ref to the data values should be updated
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, key, config); err != nil {
					return false
				}
				Expect(config.Status.SecretRef).To(Equal(fmt.Sprintf("%s-%s-data-values", clusterName, constants.KubevipCloudProviderAddonName)))
				return true
			})
		})
	})

})
