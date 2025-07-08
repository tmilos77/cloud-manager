package bootstrap

import (
	cloudcontrolv1beta1 "github.com/kyma-project/cloud-manager/api/cloud-control/v1beta1"
	cloudresourcesv1beta1 "github.com/kyma-project/cloud-manager/api/cloud-resources/v1beta1"
	"github.com/kyma-project/cloud-manager/pkg/common/abstractions"
	"github.com/kyma-project/cloud-manager/pkg/config"
	jobberconfig "github.com/kyma-project/cloud-manager/pkg/jobber/config"
	awsconfig "github.com/kyma-project/cloud-manager/pkg/kcp/provider/aws/config"
	azureconfig "github.com/kyma-project/cloud-manager/pkg/kcp/provider/azure/config"
	cceeconfig "github.com/kyma-project/cloud-manager/pkg/kcp/provider/ccee/config"
	gcpclient "github.com/kyma-project/cloud-manager/pkg/kcp/provider/gcp/client"
	"github.com/kyma-project/cloud-manager/pkg/kcp/scope"
	vpcpeeringconfig "github.com/kyma-project/cloud-manager/pkg/kcp/vpcpeering/config"
	"github.com/kyma-project/cloud-manager/pkg/quota"
	skrruntimeconfig "github.com/kyma-project/cloud-manager/pkg/skr/runtime/config"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	KcpScheme = runtime.NewScheme()
	SkrScheme = runtime.NewScheme()
	SetupLog  = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(KcpScheme))
	utilruntime.Must(cloudcontrolv1beta1.AddToScheme(KcpScheme))
	utilruntime.Must(apiextensions.AddToScheme(KcpScheme))

	utilruntime.Must(clientgoscheme.AddToScheme(SkrScheme))
	utilruntime.Must(cloudresourcesv1beta1.AddToScheme(SkrScheme))
	utilruntime.Must(apiextensions.AddToScheme(SkrScheme))
}

func LoadConfig() config.Config {
	env := abstractions.NewOSEnvironment()
	configDir := env.Get("CONFIG_DIR")
	if len(configDir) < 1 {
		configDir = "./config/config"
	}
	cfg := config.NewConfig(env)
	cfg.BaseDir(configDir)

	awsconfig.InitConfig(cfg)
	azureconfig.InitConfig(cfg)
	cceeconfig.InitConfig(cfg)
	quota.InitConfig(cfg)
	skrruntimeconfig.InitConfig(cfg)
	scope.InitConfig(cfg)
	gcpclient.InitConfig(cfg)
	vpcpeeringconfig.InitConfig(cfg)
	jobberconfig.InitConfig(cfg)

	cfg.Read()

	return cfg
}
