package jobber

import (
	"context"
	"flag"
	"os"

	"github.com/elliotchance/pie/v2"
	cloudcontrolv1beta1 "github.com/kyma-project/cloud-manager/api/cloud-control/v1beta1"
	cloudresourcesv1beta1 "github.com/kyma-project/cloud-manager/api/cloud-resources/v1beta1"
	"github.com/kyma-project/cloud-manager/pkg/common/bootstrap"
	"github.com/kyma-project/cloud-manager/pkg/composed"
	"github.com/kyma-project/cloud-manager/pkg/feature"
	featuretypes "github.com/kyma-project/cloud-manager/pkg/feature/types"
	"github.com/kyma-project/cloud-manager/pkg/jobber"
	"github.com/kyma-project/cloud-manager/pkg/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func JobberMain() {
	var metricsAddr string
	var probeAddr string
	var gcpStructuredLogging bool
	//var apiVersion string
	//var kind string
	//var namespace string
	//var name string
	fs := flag.NewFlagSet("jobber", flag.ExitOnError)
	fs.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	fs.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	fs.BoolVar(&gcpStructuredLogging, "gcp-structured-logging", false, "Enable GCP structured logging")

	jobberRunOptions := jobber.NewRunOptions()
	jobberRunOptions.RegisterFlags(fs)
	// Ignore errors; CommandLine is set for ExitOnError.
	_ = fs.Parse(os.Args[2:])

	cfg := bootstrap.LoadConfig()
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

	bootstrap.SetupLog.WithValues(
		"scheme", "KCP",
		"kinds", pie.Keys(bootstrap.KcpScheme.KnownTypes(cloudcontrolv1beta1.GroupVersion)),
	).Info("Schema dump")
	bootstrap.SetupLog.WithValues(
		"scheme", "SKR",
		"kinds", pie.Keys(bootstrap.SkrScheme.KnownTypes(cloudresourcesv1beta1.GroupVersion)),
	).Info("Schema dump")
	bootstrap.SetupLog.WithValues("config", cfg.PrintJson()).
		Info("Config dump")

	bootstrap.SetupLog.
		WithValues(jobberRunOptions.LoggerValues()...).
		Info("Starting jobber")
	co, err := jobberRunOptions.Complete(baseCtx)
	if err != nil {
		bootstrap.SetupLog.Error(err, "Error completing jobber options")
		os.Exit(1)
	}
	err = jobber.Run(baseCtx, co)
	if err != nil {
		bootstrap.SetupLog.
			WithValues("error", err).
			WithValues(jobberRunOptions.LoggerValues()...).
			Error(err, "Error running jobber")
	} else {
		bootstrap.SetupLog.
			WithValues(jobberRunOptions.LoggerValues()...).
			Info("Jobber finished")
	}
}
