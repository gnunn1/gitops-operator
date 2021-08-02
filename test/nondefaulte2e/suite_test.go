/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package nondefaulte2e

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"testing"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	argoapi "github.com/argoproj-labs/argocd-operator/pkg/apis"
	argoapp "github.com/argoproj-labs/argocd-operator/pkg/apis/argoproj/v1alpha1"
	argocdprovisioner "github.com/argoproj-labs/argocd-operator/pkg/controller/argocd"
	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appsv1 "github.com/openshift/api/apps/v1"
	configv1 "github.com/openshift/api/config/v1"
	console "github.com/openshift/api/console/v1"
	oauthv1 "github.com/openshift/api/oauth/v1"
	routev1 "github.com/openshift/api/route/v1"
	templatev1 "github.com/openshift/api/template/v1"
	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	pipelinesv1alpha1 "github.com/redhat-developer/gitops-operator/api/v1alpha1"
	"github.com/redhat-developer/gitops-operator/common"
	"github.com/redhat-developer/gitops-operator/controllers/argocd"
	"github.com/redhat-developer/gitops-operator/controllers/argocdmetrics"
	"github.com/redhat-developer/gitops-operator/controllers/gitopsservice"
	"github.com/redhat-developer/gitops-operator/test/helper"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment

const (
	operatorName              = "gitops-operator"
	argoCDConfigMapName       = "argocd-cm"
	argoCDRouteName           = "openshift-gitops-server"
	argoCDNamespace           = "openshift-gitops"
	authURL                   = "/auth/realms/master/protocol/openid-connect/token"
	depracatedArgoCDNamespace = "openshift-pipelines-app-delivery"
	consoleLinkName           = "argocd"
	argoCDInstanceName        = "openshift-gitops"
	gitopsInstanceName        = "cluster"
	defaultKeycloakIdentifier = "keycloak"
	defaultTemplateIdentifier = "rhsso"
	realmURL                  = "/auth/admin/realms/argocd"
	rhssosecret               = "keycloak-secret"
	clusterConfigEnv          = "ARGOCD_CLUSTER_CONFIG_NAMESPACES"
	disableDexEnv             = "DISABLE_DEX"
	timeout                   = time.Second * 60
	duration                  = time.Second * 10
	interval                  = time.Millisecond * 250
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Controller Suite",
		[]Reporter{printer.NewlineReporter{}})
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	useActualCluster := true
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("../..", "config", "crd", "bases"),
		},
		UseExistingCluster:    &useActualCluster, // use an actual OpenShift cluster specified in kubeconfig
		ErrorIfCRDPathMissing: true,
	}
	// disable default argocd instance
	Expect(os.Setenv(common.DisableDefaultInstallEnvVar, "true")).To(Succeed())
	// disable dex by default
	Expect(os.Setenv(disableDexEnv, "true")).To(Succeed())

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = pipelinesv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	Expect(routev1.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
	Expect(argoapi.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
	Expect(monitoringv1.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
	Expect(operatorsv1.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
	Expect(operatorsv1alpha1.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
	Expect(console.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
	Expect(oauthv1.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
	Expect(configv1.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
	Expect(templatev1.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
	Expect(appsv1.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	err = helper.EnsureCleanSlate(k8sClient)
	Expect(err).NotTo(HaveOccurred())
	err = helper.DeleteNamespace(k8sClient, "openshift-gitops")
	Expect(err).NotTo(HaveOccurred())

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).NotTo(HaveOccurred())

	err = (&gitopsservice.ReconcileGitopsService{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	err = (&argocd.ReconcileArgoCDRoute{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	err = (&argocdmetrics.ArgoCDMetricsReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	err = argocdprovisioner.Add(mgr)
	Expect(err).NotTo(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = mgr.Start(ctrl.SetupSignalHandler())
		Expect(err).NotTo(HaveOccurred())
	}()

}, 60)

var _ = AfterSuite(func() {
	By("remove the GitOpsService Instance")
	existingGitOpsInstance := &pipelinesv1alpha1.GitopsService{}
	checkIfPresent(types.NamespacedName{Name: gitopsInstanceName}, existingGitOpsInstance)

	err := k8sClient.Delete(context.TODO(), existingGitOpsInstance)
	Expect(err).NotTo(HaveOccurred())

	checkIfDeleted(types.NamespacedName{Name: gitopsInstanceName}, existingGitOpsInstance)

	By("check if the default Argo CD instance is removed")
	checkIfDeleted(types.NamespacedName{Name: argoCDInstanceName, Namespace: argoCDNamespace}, &argoapp.ArgoCD{})

	Expect(os.Unsetenv(common.DisableDefaultInstallEnvVar)).To(Succeed())
	Expect(os.Unsetenv(disableDexEnv)).To(Succeed())

	By("tearing down the test environment")
	err = testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

// checks if a given resource is present in the cluster
// continouslly polls until it returns nil or a timeout occurs
func checkIfPresent(ns types.NamespacedName, obj runtime.Object) {
	Eventually(func() error {
		err := k8sClient.Get(context.TODO(), ns, obj)
		if err != nil {
			return err
		}
		return nil
	}, timeout, interval).ShouldNot(HaveOccurred())
}

// checks if a given resource is deleted
// continouslly polls until the object is deleted or a timeout occurs
func checkIfDeleted(ns types.NamespacedName, obj runtime.Object) {
	Eventually(func() error {
		err := k8sClient.Get(context.TODO(), ns, obj)
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}, timeout, interval).ShouldNot(HaveOccurred())
}
