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

package main

import (
	"embed"
	"encoding/json"
	"flag"
	"os"

	util "github.com/openyurtio/yurt-edgex-manager/controllers/utils"
	edgexwebhook "github.com/openyurtio/yurt-edgex-manager/pkg/webhook/edgex"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	unitv1alpha1 "github.com/openyurtio/api/apps/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	devicev1alpha1 "github.com/openyurtio/yurt-edgex-manager/api/v1alpha1"
	devicev1alpha2 "github.com/openyurtio/yurt-edgex-manager/api/v1alpha2"
	"github.com/openyurtio/yurt-edgex-manager/controllers"
	//+kubebuilder:scaffold:imports
)

var (
	scheme       = runtime.NewScheme()
	setupLog     = ctrl.Log.WithName("setup")
	securityFile = "EdgeXConfig/config.json"
	nosectyFile  = "EdgeXConfig/config-nosecty.json"
	manifestPath = "EdgeXConfig/manifest.yaml"
	//go:embed EdgeXConfig
	edgeXconfig embed.FS
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(devicev1alpha1.AddToScheme(scheme))

	utilruntime.Must(devicev1alpha2.AddToScheme(scheme))

	utilruntime.Must(unitv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var enableWebhook bool
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&enableWebhook, "enable-webhook", false,
		"Enable webhook for controller manager. "+
			"Enabling this will ensure edgex resource validation.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	securityContent, err := edgeXconfig.ReadFile(securityFile)
	if err != nil {
		setupLog.Error(err, "File to open the embed EdgeX security config")
		os.Exit(1)
	}
	nosectyContent, err := edgeXconfig.ReadFile(nosectyFile)
	if err != nil {
		setupLog.Error(err, "File to open the embed EdgeX nosecty config")
	}

	var (
		edgexconfig        = controllers.EdgeXConfig{}
		edgexnosectyconfig = controllers.EdgeXConfig{}
	)

	err = json.Unmarshal(securityContent, &edgexconfig)
	if err != nil {
		setupLog.Error(err, "Error security edgeX configuration file")
		os.Exit(1)
	}
	for _, version := range edgexconfig.Versions {
		controllers.SecurityComponents[version.Name] = version.Components
		controllers.SecurityConfigMaps[version.Name] = version.ConfigMaps
	}

	err = json.Unmarshal(nosectyContent, &edgexnosectyconfig)
	if err != nil {
		setupLog.Error(err, "Error nosecty edgeX configuration file")
		os.Exit(1)
	}
	for _, version := range edgexnosectyconfig.Versions {
		controllers.NoSectyComponents[version.Name] = version.Components
		controllers.NoSectyConfigMaps[version.Name] = version.ConfigMaps
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "31095ea9.openyurt.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// register the field indexers
	setupLog.Info("registering the field indexers")
	if err := util.RegisterFieldIndexers(mgr.GetFieldIndexer()); err != nil {
		setupLog.Error(err, "failed to register field indexers")
		os.Exit(1)
	}

	if err = (&controllers.EdgeXReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "EdgeX")
		os.Exit(1)
	}

	if enableWebhook {
		manifestContent, err := edgeXconfig.ReadFile(manifestPath)
		if err != nil {
			setupLog.Error(err, "File to open the embed EdgeX manifest config")
			os.Exit(1)
		}
		if err = (&edgexwebhook.EdgeXHandler{Client: mgr.GetClient(), ManifestContent: manifestContent}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "EdgeX")
			os.Exit(1)
		}
	} else {
		setupLog.Info("webhook disabled")
	}

	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
