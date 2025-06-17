package main

import (
	"context"
	"flag"
	"os"
	"time"

	"github.com/elliotchance/pie/v2"
	cloudcontrolv1beta1 "github.com/kyma-project/cloud-manager/api/cloud-control/v1beta1"
	cloudresourcesv1beta1 "github.com/kyma-project/cloud-manager/api/cloud-resources/v1beta1"
	"github.com/kyma-project/cloud-manager/pkg/composed"
	"github.com/kyma-project/cloud-manager/pkg/feature"
	featuretypes "github.com/kyma-project/cloud-manager/pkg/feature/types"
	"github.com/kyma-project/cloud-manager/pkg/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func jobber() {
	var metricsAddr string
	var probeAddr string
	var gcpStructuredLogging bool
	var apiVersion string
	var kind string
	var namespace string
	var name string
	fs := flag.NewFlagSet("jobber", flag.ExitOnError)
	fs.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	fs.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	fs.BoolVar(&gcpStructuredLogging, "gcp-structured-logging", false, "Enable GCP structured logging")
	fs.StringVar(&apiVersion, "api-version", "", "API version")
	fs.StringVar(&kind, "kind", "", "Kind")
	fs.StringVar(&namespace, "namespace", "", "Namespace")
	fs.StringVar(&name, "name", "", "Name")
	args := os.Args[2:]
	_ = fs.Parse(args)

	cfg := loadConfig()
	cfg.Read()

	opts := zap.Options{}
	if gcpStructuredLogging {
		opts.EncoderConfigOptions = []zap.EncoderConfigOption{
			util.GcpZapEncoderConfigOption(),
		}
	} else {
		opts.Development = true
	}

	baseCtx := context.Background()
	baseCtx = feature.ContextBuilderFromCtx(baseCtx).
		Landscape(os.Getenv("LANDSCAPE")).
		Plane(featuretypes.PlaneKcp).
		Build(baseCtx)

	rootLogger := zap.New(zap.UseFlagOptions(&opts))
	rootLogger = rootLogger.WithSink(util.NewLogFilterSink(rootLogger.GetSink()))
	baseCtx = composed.LoggerIntoCtx(baseCtx, rootLogger)
	ctrl.SetLogger(rootLogger)

	setupLog.WithValues(
		"scheme", "KCP",
		"kinds", pie.Keys(kcpScheme.KnownTypes(cloudcontrolv1beta1.GroupVersion)),
	).Info("Schema dump")
	setupLog.WithValues(
		"scheme", "SKR",
		"kinds", pie.Keys(skrScheme.KnownTypes(cloudresourcesv1beta1.GroupVersion)),
	).Info("Schema dump")
	setupLog.WithValues("config", cfg.PrintJson()).
		Info("Config dump")


	setupLog.
		WithValues(
			"apiVersion", apiVersion,
			"kind", kind,
			"namespace", namespace,
			"name", name,
		).
		Info("Starting jobber")
	time.Sleep(10 * time.Second)
	setupLog.
		WithValues(
			"apiVersion", apiVersion,
			"kind", kind,
			"namespace", namespace,
			"name", name,
		).
		Info("Jobber finished")
}
