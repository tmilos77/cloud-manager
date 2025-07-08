package jobber

import (
	"context"
	"flag"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	cloudcontrolv1beta1 "github.com/kyma-project/cloud-manager/api/cloud-control/v1beta1"
	"github.com/kyma-project/cloud-manager/pkg/common/abstractions"
	"github.com/kyma-project/cloud-manager/pkg/common/bootstrap"
	"github.com/kyma-project/cloud-manager/pkg/composed"
	featuretypes "github.com/kyma-project/cloud-manager/pkg/feature/types"
	skrmanager "github.com/kyma-project/cloud-manager/pkg/skr/runtime/manager"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type RunOptions struct {
	Env        abstractions.Environment
	ScopeName  string
	ApiVersion string
	Kind       string
	Namespace  string
	Name       string
	Plane      featuretypes.PlaneName
}

func NewRunOptions() *RunOptions {
	return &RunOptions{}
}

func (o *RunOptions) RegisterFlags(fs *flag.FlagSet) *RunOptions {
	fs.StringVar(&o.ScopeName, "scope", "", "Scope name")
	fs.StringVar(&o.ApiVersion, "api-version", "", "API version")
	fs.StringVar(&o.Kind, "kind", "", "Kind")
	fs.StringVar(&o.Namespace, "namespace", "kcp-system", "Namespace")
	fs.StringVar(&o.Name, "name", "", "Name")
	fs.StringVar(&o.Plane, "plane", "", "Plane - KCP or SKR")

	return o
}

func (o *RunOptions) WithEnv(env abstractions.Environment) *RunOptions {
	o.Env = env
	return o
}

func (o *RunOptions) LoggerValues() []any {
	return []any{
		"scopeName", o.ScopeName,
		"apiVersion", o.ApiVersion,
		"kind", o.Kind,
		"namespace", o.Namespace,
		"name", o.Name,
		"plane", o.Plane,
	}
}

func (o *RunOptions) Complete(ctx context.Context) (*CompletedRunOptions, error) {
	if o.Env == nil {
		o.Env = abstractions.NewOSEnvironment()
	}

	var result error
	if o.ApiVersion == "" {
		result = multierror.Append(result, fmt.Errorf("--api-version must be specified"))
	}
	if o.Kind == "" {
		result = multierror.Append(result, fmt.Errorf("--kind must be specified"))
	}
	if o.Name == "" {
		result = multierror.Append(result, fmt.Errorf("--name must be specified"))
	}
	if o.Namespace == "" {
		o.Namespace = o.Env.Get("NAMESPACE")
	}
	if o.Namespace == "" {
		result = multierror.Append(result, fmt.Errorf("--namespace or NAMESPACE env var must be specified"))
	}
	if o.Plane == "" {
		o.Plane = o.determinePlane()
	}
	if o.Plane == "" {
		result = multierror.Append(result, fmt.Errorf("--plane must be specified or be able to get determined from the group"))
	}

	if result != nil {
		return nil, result
	}

	kcpClient, err := o.createKcpClient()
	if err != nil {
		result = multierror.Append(result, err)
	}

	scope, err := o.loadScope(ctx, kcpClient)
	if err != nil {
		result = multierror.Append(result, err)
	}

	var skrClient client.Client
	if o.shouldCreateSkrClient() {
		skrClient, err = o.createSkrClient(ctx, kcpClient)
		if err != nil {
			result = multierror.Append(result, err)
		}
	}

	gv, err := schema.ParseGroupVersion(o.ApiVersion)
	if err != nil {
		result = multierror.Append(result, fmt.Errorf("error parsing apVersion %q", o.ApiVersion))
	}

	if result != nil {
		return nil, result
	}

	gvk := gv.WithKind(o.Kind)

	scheme := bootstrap.KcpScheme
	planeClient := kcpClient
	if o.Plane == featuretypes.PlaneSkr {
		scheme = bootstrap.SkrScheme
		planeClient = skrClient
	}

	tp, ok := scheme.KnownTypes(gvk.GroupVersion())[o.Kind]
	if !ok {
		return nil, fmt.Errorf("gvk %q not found in the scheme", gvk.String())
	}
	objIntf := reflect.New(tp).Interface()
	obj, ok := objIntf.(JobbedObject)
	if !ok {
		return nil, fmt.Errorf("object %T is not a JobbedObject", objIntf)
	}

	return &CompletedRunOptions{
		gvk:       gvk,
		obj:       obj,
		namespace: o.Namespace,
		name:      o.Name,

		scope:       scope,
		kcpClient:   kcpClient,
		skrClient:   skrClient,
		planeClient: planeClient,
	}, nil
}

func (o *RunOptions) determinePlane() featuretypes.PlaneName {
	if strings.HasPrefix(o.ApiVersion, cloudcontrolv1beta1.GroupVersion.Group) {
		return featuretypes.PlaneKcp
	} else if strings.HasPrefix(o.ApiVersion, cloudcontrolv1beta1.GroupVersion.Group) {
		return featuretypes.PlaneSkr
	}
	return ""
}

func (o *RunOptions) shouldCreateSkrClient() bool {
	return o.Plane == featuretypes.PlaneSkr
}

func (o *RunOptions) createKcpClient() (client.Client, error) {
	clnt, err := client.New(ctrl.GetConfigOrDie(), client.Options{
		Scheme: bootstrap.KcpScheme,
	})
	return clnt, err
}

func (o *RunOptions) loadScope(ctx context.Context, kcpClient client.Client) (*cloudcontrolv1beta1.Scope, error) {
	if o.ScopeName == "" {
		return nil, fmt.Errorf("--scope must be specified")
	}

	scope := &cloudcontrolv1beta1.Scope{}
	err := kcpClient.Get(ctx, client.ObjectKey{Namespace: o.Namespace, Name: o.ScopeName}, scope)
	if err != nil {
		return nil, fmt.Errorf("error loading scope: %w", err)
	}

	return scope, nil
}

func (o *RunOptions) createSkrClient(ctx context.Context, kcpClient client.Client) (client.Client, error) {
	if o.Namespace == "" {
		return nil, fmt.Errorf("unable to create skr client w/out namespace")
	}
	skrManagerFactory := skrmanager.NewFactory(kcpClient, o.Namespace, bootstrap.SkrScheme)
	restConfig, err := skrManagerFactory.LoadRestConfig(ctx, fmt.Sprintf("kubeconfig-%s", o.ScopeName), "config")
	if err != nil {
		return nil, fmt.Errorf("error loading rest config: %w", err)
	}

	skrClient, err := client.New(restConfig, client.Options{Scheme: bootstrap.SkrScheme})
	if err != nil {
		return nil, fmt.Errorf("error creating skr client: %w", err)
	}

	return skrClient, nil
}

// CompletedRunOptions ======================

type CompletedRunOptions struct {
	gvk       schema.GroupVersionKind
	scheme    *runtime.Scheme
	obj       JobbedObject
	namespace string
	name      string

	scope       *cloudcontrolv1beta1.Scope
	kcpClient   client.Client
	skrClient   client.Client
	planeClient client.Client
}

func Run(ctx context.Context, opts *CompletedRunOptions) error {
	logger := composed.LoggerFromCtx(ctx)
	logger.Info("Jobber running long operations")
	time.Sleep(time.Minute)

	logger.Info("Jobber updating LastReconciledGeneration")
	err := opts.planeClient.Get(ctx, client.ObjectKey{Namespace: opts.namespace, Name: opts.name}, opts.obj)
	if err != nil {
		return fmt.Errorf("error loading object: %w", err)
	}

	opts.obj.SetLastReconciledGeneration(opts.obj.GetGeneration())
	err = composed.PatchObjStatus(ctx, opts.obj, opts.planeClient)

	return err
}
