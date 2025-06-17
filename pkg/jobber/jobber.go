package jobber

import (
	"context"
	"fmt"

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

	LastReconciledGeneration() int64
	JobberID() string
	JobberPodName() string
}

//type Record struct {
//	ID         string `gorm:"primary_key"`
//	Generation int64
//	Manifest   string
//}
//
//type Repository interface {
//	Load(ctx context.Context, id string) (*Record, error)
//	Save(ctx context.Context, rec *Record) error
//}

type jobber struct {
	obj     JobbedObject
	client  client.Client
	//repo    Repository
	options Options

	loadedObj JobbedObject
}

func (j *jobber) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	obj := j.obj.DeepCopyObject().(JobbedObject)
	err := j.client.Get(ctx, req.NamespacedName, obj)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	j.loadedObj = obj

	if obj.GetGeneration() >= obj.LastReconciledGeneration() {
		return ctrl.Result{}, nil
	}

	pod := &corev1.Pod{}
	err = j.client.Get(ctx, types.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.JobberPodName(),
	}, pod)
	if client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, fmt.Errorf("failed to load jobber pod: %w", err)
	}

	if err == nil {
		// pod still exists
		if !(pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed) {
			// still running

			if !j.options.IsPodRunningTimeout(pod) {
				// keep it running
				return ctrl.Result{Requeue: true}, nil
			}
		}

		// delete finished or timed-out pod
		err = j.client.Delete(ctx, pod)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to delete jobber pod: %w", err)
		}

		return ctrl.Result{Requeue: true}, nil
	}

	pod = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: obj.GetNamespace(),
			Name:      obj.JobberPodName(),
			Annotations: map[string]string{
				AnnotationObjectGeneration: fmt.Sprintf("%d", obj.GetGeneration()),
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:            "jobber",
					Image:           j.options.Image,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command:         []string{
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
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("200Mi"),
						},
					},
					Env:     j.options.EnvVars(),
					EnvFrom: []corev1.EnvFromSource{},
					SecurityContext: &corev1.SecurityContext{
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{"ALL"},
						},
					},
					VolumeMounts:   j.options.VolumeMounts(),
					ReadinessProbe: j.options.ReadinessProbe(),
					LivenessProbe:  j.options.LivenessProbe(),
				},
			},
			RestartPolicy:                 corev1.RestartPolicyNever,
			Volumes:                       j.options.Volumes(),
			TerminationGracePeriodSeconds: ptr.To(j.options.TerminationGracePeriodSeconds),
		},
	}

	err = j.client.Create(ctx, pod)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create jobber pod: %w", err)
	}

	return ctrl.Result{RequeueAfter: util.Timing.T10000ms()}, nil
}
