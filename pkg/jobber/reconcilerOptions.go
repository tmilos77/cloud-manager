package jobber

import (
	"time"

	jobberconfig "github.com/kyma-project/cloud-manager/pkg/jobber/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/clock"
)

type ReconcilerOption func(o *reconcilerOptions)

type reconcilerOptions struct {
	clock              clock.Clock
	image              string
	namespace          string
	serviceAccountName string

	scopeName string

	podRunningTimeout             time.Duration
	terminationGracePeriodSeconds int64

	readinessInitialDelaySeconds int32
	readinessPeriodSeconds       int32
	readinessPath                string
	readinessPort                int32

	livenessInitialDelaySeconds int32
	livenessPeriodSeconds       int32
	livenessPath                string
	livenessPort                int32

	configMapConfig        string // cloud-manager-config
	configMapEnv           string // cloud-manager-env
	secretGcpCredentials   string
	secretAwsCredentials   string
	secretAzureCredentials string
	secretCceeCredentials  string
}

func newOptions(opts ...ReconcilerOption) *reconcilerOptions {
	o := &reconcilerOptions{}
	withDefaults(o)
	for _, opt := range opts {
		opt(o)
	}
	return o
}

func WithScopeName(scopeName string) ReconcilerOption {
	return func(o *reconcilerOptions) {
		o.scopeName = scopeName
	}
}

func WithClock(clock clock.Clock) ReconcilerOption {
	return func(o *reconcilerOptions) {
		o.clock = clock
	}
}

func WithPodRunningTimeout(to time.Duration) ReconcilerOption {
	return func(o *reconcilerOptions) {
		o.podRunningTimeout = to
	}
}

func withDefaults(o *reconcilerOptions) {
	o.clock = &clock.RealClock{}
	o.image = jobberconfig.JobberConfig.Image
	o.namespace = jobberconfig.JobberConfig.Namespace
	o.serviceAccountName = jobberconfig.JobberConfig.ServiceAccountName
	o.podRunningTimeout = jobberconfig.JobberConfig.PodRunningTimeout
	o.terminationGracePeriodSeconds = jobberconfig.JobberConfig.TerminationGracePeriodSeconds
	o.configMapConfig = jobberconfig.JobberConfig.ConfigMapConfig
	o.configMapEnv = jobberconfig.JobberConfig.ConfigMapEnv
	o.secretGcpCredentials = jobberconfig.JobberConfig.SecretGcpCredentials
	o.secretAwsCredentials = jobberconfig.JobberConfig.SecretAwsCredentials
	o.secretAzureCredentials = jobberconfig.JobberConfig.SecretAzureCredentials
	o.secretCceeCredentials = jobberconfig.JobberConfig.SecretCceeCredentials
}

func (o *reconcilerOptions) isPodRunningTimeout(pod *corev1.Pod) bool {
	duration := o.clock.Since(pod.CreationTimestamp.Time)
	if duration > o.podRunningTimeout {
		return true
	}
	return false
}

func (o *reconcilerOptions) volumes() []corev1.Volume {
	return []corev1.Volume{
		{
			Name: "gcp-credentials",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: o.secretGcpCredentials,
				},
			},
		},
		{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{
						{
							ConfigMap: &corev1.ConfigMapProjection{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: o.configMapConfig,
								},
							},
						},
						{
							Secret: &corev1.SecretProjection{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: o.secretAwsCredentials,
								},
							},
						},
						{
							Secret: &corev1.SecretProjection{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: o.secretAzureCredentials,
								},
							},
						},
						{
							Secret: &corev1.SecretProjection{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: o.secretCceeCredentials,
								},
							},
						},
					},
				},
			},
		},
	}
}

func (o *reconcilerOptions) volumeMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{
			Name:      "gcp-credentials",
			MountPath: "/var/run/secrets/cloud-manager.kyma-project.io/gcp",
			ReadOnly:  true,
		},
		{
			Name:      "config",
			MountPath: "/var/cloud-manager.kyma-project.io/config",
		},
	}
}

func (o *reconcilerOptions) envVars() []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  "GCP_SA_JSON_KEY_PATH",
			Value: "/var/run/secrets/cloud-manager.kyma-project.io/gcp/credentials.json",
		},
		{
			Name:  "GCP_VPC_PEERING_KEY_PATH",
			Value: "/var/run/secrets/cloud-manager.kyma-project.io/gcp/credentials-vpcpeering.json",
		},
		{
			Name:  "CONFIG_DIR",
			Value: "/var/cloud-manager.kyma-project.io/config",
		},
		{
			Name:  "FEATURE_FLAG_CONFIG_FILE",
			Value: "/var/cloud-manager.kyma-project.io/config/featureFlags.yaml",
		},
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
		{
			Name: "NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		},
	}
}

func (o *reconcilerOptions) envFrom() []corev1.EnvFromSource {
	return []corev1.EnvFromSource{
		{
			ConfigMapRef: &corev1.ConfigMapEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: o.configMapEnv,
				},
			},
		},
	}
}

func (o *reconcilerOptions) readinessProbe() *corev1.Probe {
	return &corev1.Probe{
		InitialDelaySeconds: jobberconfig.JobberConfig.ReadinessProbe.InitialDelaySeconds, // 5
		PeriodSeconds:       jobberconfig.JobberConfig.ReadinessProbe.PeriodSeconds,       // 10
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: jobberconfig.JobberConfig.ReadinessProbe.Path,                   // /readyz
				Port: intstr.FromInt32(jobberconfig.JobberConfig.ReadinessProbe.Port), // 8081
			},
		},
	}
}

func (o *reconcilerOptions) livenessProbe() *corev1.Probe {
	return &corev1.Probe{
		InitialDelaySeconds: jobberconfig.JobberConfig.LivenessProbe.InitialDelaySeconds, // 15
		PeriodSeconds:       jobberconfig.JobberConfig.LivenessProbe.PeriodSeconds,       // 20
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: jobberconfig.JobberConfig.LivenessProbe.Path,                   // /healthz
				Port: intstr.FromInt32(jobberconfig.JobberConfig.LivenessProbe.Port), // 8081
			},
		},
	}
}
