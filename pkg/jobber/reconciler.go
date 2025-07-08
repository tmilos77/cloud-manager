package jobber

import (
	"context"
	"fmt"

	"github.com/kyma-project/cloud-manager/pkg/common/actions/focal"
	"github.com/kyma-project/cloud-manager/pkg/composed"
	"github.com/kyma-project/cloud-manager/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	AnnotationObjectGeneration = "cloud-manager.kyma-project.io/object-generation"
)

type JobbedObject interface {
	client.Object
	schema.ObjectKind

	GetLastReconciledGeneration() int64
	SetLastReconciledGeneration(generation int64)
	JobberID() string
	JobberPodName() string
}

type Reconciler interface {
	Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error)
}

func NewReconciler(obj JobbedObject, podClient client.Client, objClient client.Client, opts ...ReconcilerOption) Reconciler {
	o := newOptions(opts...)
	return &reconciler{
		obj:       obj,
		objClient: objClient,
		podClient: podClient,
		options:   o,
	}
}

type reconciler struct {
	obj       JobbedObject
	podClient client.Client
	objClient client.Client
	options   *reconcilerOptions

	loadedObj JobbedObject
}

func (j *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := composed.LoggerFromCtx(ctx)
	obj := j.obj.DeepCopyObject().(JobbedObject)
	err := j.objClient.Get(ctx, req.NamespacedName, obj)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	j.loadedObj = obj

	ns := j.options.namespace
	if ns == "" {
		ns = obj.GetNamespace()
	}
	if ns == "" {
		return ctrl.Result{}, fmt.Errorf("namespace is required")
	}

	pod := &corev1.Pod{}
	err = j.podClient.Get(ctx, types.NamespacedName{
		Namespace: j.options.namespace,
		Name:      obj.JobberPodName(),
	}, pod)
	if client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, fmt.Errorf("failed to load jobber pod: %w", err)
	}

	if err == nil {
		logger.WithValues("phase", pod.Status.Phase).Info("jobber pod already exists")
		// pod still exists
		if !(pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed) {
			// still running

			logger.Info("jobber pod still running")
			if !j.options.isPodRunningTimeout(pod) {
				// keep it running
				return ctrl.Result{Requeue: true}, nil
			}
			logger.Info("jobber pod timeout")
		}

		logger.Info("deleting jobber pod")

		// delete finished or timed-out pod
		err = j.podClient.Delete(ctx, pod)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to delete jobber pod: %w", err)
		}

		return ctrl.Result{Requeue: true}, nil
	}

	if obj.GetGeneration() <= obj.GetLastReconciledGeneration() {
		logger.Info("jobber object has not changed")
		return ctrl.Result{}, nil
	}

	scopeName := j.options.scopeName
	if scopeName == "" {
		switch x := obj.(type) {
		case focal.CommonObject:
			scopeName = x.ScopeRef().Name
		}
	}
	if scopeName == "" {
		return ctrl.Result{}, fmt.Errorf("unable to determine scope for type %T", obj)
	}

	pod = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: j.options.namespace,
			Name:      obj.JobberPodName(),
			Annotations: map[string]string{
				AnnotationObjectGeneration: fmt.Sprintf("%d", obj.GetGeneration()),
			},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: j.options.serviceAccountName,
			Containers: []corev1.Container{
				{
					Name:            "jobber",
					Image:           j.options.image,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command: []string{
						"/manager",
					},
					Args: []string{
						"jobber",
						"--gcp-structured-logging",
						"--api-version",
						obj.GroupVersionKind().GroupVersion().String(),
						"--kind",
						obj.GetObjectKind().GroupVersionKind().Kind,
						"--namespace",
						obj.GetNamespace(),
						"--name",
						obj.GetName(),
						"--scope",
						scopeName,
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("200Mi"),
						},
					},
					Env:     j.options.envVars(),
					EnvFrom: []corev1.EnvFromSource{},
					SecurityContext: &corev1.SecurityContext{
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{"ALL"},
						},
					},
					VolumeMounts:   j.options.volumeMounts(),
					ReadinessProbe: j.options.readinessProbe(),
					LivenessProbe:  j.options.livenessProbe(),
				},
			},
			RestartPolicy:                 corev1.RestartPolicyNever,
			Volumes:                       j.options.volumes(),
			TerminationGracePeriodSeconds: ptr.To(j.options.terminationGracePeriodSeconds),
		},
	}

	logger.Info("jobber pod creating")

	err = j.podClient.Create(ctx, pod)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create jobber pod: %w", err)
	}

	return ctrl.Result{RequeueAfter: util.Timing.T10000ms()}, nil
}
